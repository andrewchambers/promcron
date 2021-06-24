# promcron

`promcron` is cron service which can also export prometheus metrics.

Because `promcron` uses a simple and robust time keeping algorithm,
it is not suited for thousands of jobs,
but in exchange is able to detect time keeping anomalies which are exported as a metric.

How `promcron` handles some edge cases:

- If a job is overdue, `promcron` logs it, but does not run it.
- If time jumps forward more than 30 seconds, `promcron` may miss jobs
  but attempts to log them and export time anomaly metrics.
- If time jumps backwards more than 30 seconds, `promcron` may run jobs
  multiple times, but attempts to log them and export time anomaly metrics.

## Example

/etc/promcron:
```
# All fields are mandatory
#         ┌───────────── minute (0 - 59)
#         │ ┌───────────── hour (0 - 23)
#         │ │ ┌───────────── day of the month (1 - 31)
#         │ │ │ ┌───────────── month (1 - 12, jan-dec)
#         │ │ │ │ ┌───────────── day of the week (0 - 6, mon-sun) 
#         │ │ │ │ │
#         │ │ │ │ │
job-label 0 * * * * echo 'An hour has passed'
# Repeat and range syntax
job2 */10 * * * * echo 'Every 10 minutes'
job3 0-5  * * * * echo 'First 5 minutes of each hour'
```

Run promcron:
```
$ promcron -prometheus-metrics 127.0.0.1:1234 -f /etc/promcron
```

## Example of exported metrics

The table:
```
job1 0/2 * * * * echo hello
job2 1/2 * * * * sleep 300
```

produces the following exported metrics:
```
...
# HELP promcron_backward_time_skips Detected anomalies where time moved backward causing potential job reruns.
# TYPE promcron_backward_time_skips counter
promcron_backward_time_skips 0
# HELP promcron_forward_time_skips Detected anomalies where time moved forward causing potential job skips.
# TYPE promcron_forward_time_skips counter
promcron_forward_time_skips 0
# HELP promcron_job_duration Time taken for the last job execution.
# TYPE promcron_job_duration gauge
promcron_job_duration{job="job1"} 0.004243458
promcron_job_duration{job="job2"} 0
# HELP promcron_job_failure_count Times a job has failed.
# TYPE promcron_job_failure_count counter
promcron_job_failure_count{job="job1"} 0
promcron_job_failure_count{job="job2"} 0
# HELP promcron_job_overdue_count Times a job did not finish before the next rescheduling.
# TYPE promcron_job_overdue_count counter
promcron_job_overdue_count{job="job1"} 0
promcron_job_overdue_count{job="job2"} 1
# HELP promcron_job_success_count Times a job has succeeded.
# TYPE promcron_job_success_count counter
promcron_job_success_count{job="job1"} 2
promcron_job_success_count{job="job2"} 0
# HELP promcron_running_jobs Number of jobs that are currently running.
# TYPE promcron_running_jobs gauge
promcron_running_jobs 1
```