package main

import (
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type Job struct {
	Name    string
	Command string
	Minute  uint64
	Hour    uint64
	Dom     uint64
	Month   uint64
	Dow     uint64
	wg      sync.WaitGroup
	child   *exec.Cmd
	running int32
}

func (j *Job) ShouldRunAt(t *time.Time) bool {
	if (1 << uint(t.Minute()) & j.Minute) == 0 {
		return false
	}
	if (1 << uint(t.Hour()) & j.Hour) == 0 {
		return false
	}
	if (1 << uint(t.Month()) & j.Month) == 0 {
		return false
	}
	domMatch := (1 << uint(t.Day()) & j.Dom) > 0
	dowMatch := (1 << uint(t.Weekday()) & j.Dow) > 0
	if j.Dom&starBit > 0 || j.Dow&starBit > 0 {
		return domMatch && dowMatch
	}
	return domMatch || dowMatch
}

func (j *Job) IsRunning() bool {
	return atomic.LoadInt32(&j.running) != 0
}

type OnJobExitFunc func(string, time.Duration, *exec.Cmd, error)

func (j *Job) Start(onExit OnJobExitFunc) bool {
	j.wg.Wait()
	atomic.StoreInt32(&j.running, 1)
	j.wg.Add(1)
	go func() {
		defer j.wg.Done()
		defer atomic.StoreInt32(&j.running, 0)
		j.child = exec.Command("/bin/sh", "-c", j.Command)
		j.child.Stdout = os.Stderr
		j.child.Stderr = os.Stderr
		startTime := time.Now()
		err := j.child.Run()
		endTime := time.Now()
		onExit(j.Name, endTime.Sub(startTime), j.child, err)
	}()
	return true
}

func (j *Job) Wait() {
	j.wg.Wait()
}
