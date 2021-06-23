package main

import (
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// flags
var (
	metricsAddress = flag.String("prometheus-metrics", "", "address:port to serve job prometheus metrics on.")
	tab            = flag.String("f", "/etc/promcron", "'promcron' file to load and run.")
)

// metrics
var (
	runningJobsGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "promcron_running_jobs",
		Help: "Number of jobs that are currently running.",
	})
	forwardTimeSkips = promauto.NewCounter(prometheus.CounterOpts{
		Name: "promcron_forward_time_skips",
		Help: "Detected time anomalies where time moved forward causing potential job skips.",
	})
	backwardTimeSkips = promauto.NewCounter(prometheus.CounterOpts{
		Name: "promcron_backward_time_skips",
		Help: "Detected anomalies where time moved backward causing potential job duplicates.",
	})
	overdueCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "promcron_job_overdue_count",
			Help: "Times a job did not finish before the next rescheduling.",
		},
		[]string{"job"},
	)
	failureCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "promcron_job_failure_count",
			Help: "Times a job has failed.",
		},
		[]string{"job"},
	)
	successCounter = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "promcron_job_success_count",
			Help: "Times a job has succeeded.",
		},
		[]string{"job"},
	)
	durationGauge = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "promcron_job_duration",
			Help: "Time taken for the last job execution.",
		},
		[]string{"job"},
	)
)

const delayOvershoot = 5 * time.Second

func delayTillNextCheck(fromt time.Time) time.Duration {
	return delayOvershoot + // Be resilient to minor backward clock adjustments.
		(time.Duration(60-fromt.Second()) * time.Second) -
		(time.Duration(fromt.Nanosecond()%1000000000) * time.Nanosecond)
}

func onJobExit(jobName string, duration time.Duration, code int) {
	log.Printf("job %s finished in %s with exit code %d", jobName, duration, code)
	runningJobsGauge.Dec()
	if code == 0 {
		successCounter.WithLabelValues(jobName).Inc()
	} else {
		failureCounter.WithLabelValues(jobName).Inc()
	}
	durationGauge.WithLabelValues(jobName).Set(duration.Seconds())
}

func main() {
	flag.Parse()

	tabData, err := ioutil.ReadFile(*tab)
	if err != nil {
		log.Fatalf("error reading %q: %s", *tab, err)
	}

	jobs, err := ParseJobs(*tab, string(tabData))
	if err != nil {
		log.Fatalf("%s", err)
	}

	// Init prometheus vectors with job names.
	for _, j := range jobs {
		overdueCounter.WithLabelValues(j.Name)
		failureCounter.WithLabelValues(j.Name)
		successCounter.WithLabelValues(j.Name)
		durationGauge.WithLabelValues(j.Name)
	}

	if *metricsAddress != "" {
		go func() {
			http.Handle("/metrics", promhttp.Handler())
			log.Printf("serving prometheus metrics at http://%s/metrics", *metricsAddress)
			err := http.ListenAndServe(*metricsAddress, nil)
			if err != nil {
				log.Fatalf("error running metrics server: %s", err)
			}
		}()
	}

	done := make(chan struct{}, 1)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		log.Printf("shutting down due to signal")
		close(done)
		<-sigs
		log.Fatalf("forcing shutdown due to signal")
	}()

	log.Printf("scheduling %d jobs", len(jobs))

	now := time.Now()
	delay := delayTillNextCheck(now)
	prevCheck := now.Add(delay).Add(-60 * time.Second)

scheduler:
	for {
		now = time.Now()
		delay = delayTillNextCheck(now)
		nextCheck := now.Add(delay)
		actualPrevCheck := nextCheck.Add(-60 * time.Second)

		if actualPrevCheck.Unix() != prevCheck.Unix() {
			if actualPrevCheck.After(prevCheck) {
				log.Printf("forward time jump detected, jobs may have been skipped")
				forwardTimeSkips.Inc()
			} else {
				log.Printf("backward time jump detected, jobs may be run multiple times")
				backwardTimeSkips.Inc()
			}
		}

		select {
		case <-time.After(delay):
		case <-done:
			break scheduler
		}

		for _, j := range jobs {
			if !j.ShouldRunAt(&now) {
				continue
			}
			if j.IsRunning() {
				log.Printf("job %s is overdue", j.Name)
				overdueCounter.WithLabelValues(j.Name).Inc()
				continue
			}
			log.Printf("starting job %s", j.Name)
			runningJobsGauge.Inc()
			j.Start(onJobExit)
		}

		prevCheck = nextCheck
	}

	for _, j := range jobs {
		if j.IsRunning() {
			log.Printf("waiting for job %s", j.Name)
			j.Wait()
		}
	}
}
