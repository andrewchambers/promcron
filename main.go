package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// flags
var (
	printSchedule    = flag.Bool("print-schedule", false, "Print the schedule for the next 24 hours then exit.")
	printScheduleFor = flag.Duration("print-schedule-for", 0*time.Second, "Print the schedule for the specified duration then exit.")
	metricsAddress   = flag.String("prometheus-metrics", "", "address:port to serve job prometheus metrics on.")
	tab              = flag.String("f", "/etc/promcron", "'promcron' file to load and run.")
)

// metrics
var (
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
			Name: "promcron_job_duration_seconds",
			Help: "Time taken for the last job execution.",
		},
		[]string{"job"},
	)
	maxrssBytesGauge = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "promcron_job_maxrss_bytes",
			Help: "Max rss of the last job execution.",
		},
		[]string{"job"},
	)
	utimeGauge = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "promcron_job_utime_seconds",
			Help: "User cpu time used for the last job execution.",
		},
		[]string{"job"},
	)
	stimeGauge = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "promcron_job_stime_seconds",
			Help: "System cpu time used for the last job execution.",
		},
		[]string{"job"},
	)
	runningGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "promcron_job_running",
		Help: "Whether or not the job is currently running.",
	},
		[]string{"job"})
)

func delayTillNextCheck(fromt time.Time) time.Duration {
	// Schedule for midway in the next minute to be
	// resilient to clock adjustments in both directions.
	return 30*time.Second +
		(time.Duration(60-fromt.Second()) * time.Second) -
		(time.Duration(fromt.Nanosecond()%1000000000) * time.Nanosecond)
}

func onJobExit(jobName string, duration time.Duration, cmd *exec.Cmd, err error) {

	exitStatus := 127
	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				exitStatus = status.ExitStatus()
			}
		}
	} else {
		exitStatus = 0
	}

	log.Printf("job %s finished in %s with exit status %d", jobName, duration, exitStatus)

	runningGauge.WithLabelValues(jobName).Set(0)

	if exitStatus == 0 {
		successCounter.WithLabelValues(jobName).Inc()
	} else {
		failureCounter.WithLabelValues(jobName).Inc()
	}

	durationGauge.WithLabelValues(jobName).Set(duration.Seconds())

	if rusage, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
		durationGauge.WithLabelValues(jobName).Set(duration.Seconds())
		maxrssBytesGauge.WithLabelValues(jobName).Set(float64(rusage.Maxrss * 1024))
		utimeGauge.WithLabelValues(jobName).Set(float64(rusage.Utime.Sec) + (float64(rusage.Utime.Usec) / 1000000.0))
		stimeGauge.WithLabelValues(jobName).Set(float64(rusage.Stime.Sec) + (float64(rusage.Stime.Usec) / 1000000.0))
	}

}

func printScheduleAndExit(jobs []*Job) {
	duration := 24 * time.Hour
	if *printScheduleFor != 0 {
		duration = *printScheduleFor
	}
	simulatedTime := time.Now()
	end := simulatedTime.Add(duration)
	for end.After(simulatedTime) {
		simulatedTime = simulatedTime.Add(delayTillNextCheck(simulatedTime))
		for _, j := range jobs {
			if !j.ShouldRunAt(&simulatedTime) {
				continue
			}
			fmt.Printf("%s - %s\n", simulatedTime.Format("2006/01/02 15:04"), j.Name)
		}
	}
	os.Exit(0)
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

	if *printSchedule || *printScheduleFor != 0 {
		printScheduleAndExit(jobs)
	}

	// Init prometheus vectors with job names.
	for _, j := range jobs {
		overdueCounter.WithLabelValues(j.Name)
		failureCounter.WithLabelValues(j.Name)
		successCounter.WithLabelValues(j.Name)
		durationGauge.WithLabelValues(j.Name)
		maxrssBytesGauge.WithLabelValues(j.Name)
		utimeGauge.WithLabelValues(j.Name)
		stimeGauge.WithLabelValues(j.Name)
		runningGauge.WithLabelValues(j.Name)
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
			runningGauge.WithLabelValues(j.Name).Set(1)
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
