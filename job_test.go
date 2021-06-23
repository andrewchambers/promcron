package main

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {

	type testcase struct {
		tab       string
		runTimes  []string
		skipTimes []string
	}

	matchingCases := []testcase{
		testcase{
			tab: "1 * * * * * true",
			runTimes: []string{
				"Jan 2 15:04",
				"Jan 1 12:04",
			},
		},
		testcase{
			tab: "2 0 * * * * true",
			runTimes: []string{
				"Jan 1 15:00",
			},
			skipTimes: []string{
				"Jan 1 15:01",
			},
		},
		testcase{
			tab: "3 */5 * * * * true",
			runTimes: []string{
				"Jan 1 15:00",
				"Jan 1 15:05",
				"Jan 1 15:10",
				"Jan 1 15:15",
			},
			skipTimes: []string{
				"Jan 1 15:01",
				"Jan 1 15:06",
				"Jan 1 15:11",
				"Jan 1 15:16",
			},
		},
		testcase{
			tab: "4 * * * jan * true",
			runTimes: []string{
				"Jan 1 15:00",
				"Jan 1 15:05",
			},
			skipTimes: []string{
				"Feb 1 15:00",
				"Feb 1 15:05",
			},
		},
		testcase{
			tab: "4 0,1,2,3 * * * * true",
			runTimes: []string{
				"Jan 1 15:00",
				"Jan 1 15:01",
				"Jan 1 15:02",
				"Jan 1 15:03",
			},
			skipTimes: []string{
				"Feb 1 15:04",
			},
		},
		testcase{
			tab: "4 0-3 * * * * true",
			runTimes: []string{
				"Jan 1 15:00",
				"Jan 1 15:01",
				"Jan 1 15:02",
				"Jan 1 15:03",
			},
			skipTimes: []string{
				"Feb 1 15:04",
			},
		},
		testcase{
			tab: "4 2/1 * * * * true",
			runTimes: []string{
				"Jan 1 15:02",
				"Jan 1 15:03",
				"Jan 1 15:11",
				"Jan 1 15:59",
			},
			skipTimes: []string{
				"Feb 1 15:00",
				"Feb 1 15:01",
			},
		},
	}

	for _, tc := range matchingCases {
		jobs, err := ParseJobs("test", tc.tab)
		if err != nil {
			t.Fatal(err)
		}
		tfmt := "Jan _2 15:04"
		for _, j := range jobs {
			for _, ts := range tc.runTimes {
				parsedTime, err := time.Parse(tfmt, ts)
				if err != nil {
					t.Fatal(err)
				}
				if !j.ShouldRunAt(&parsedTime) {
					t.Fatalf("job %s should run at %s", j.Name, parsedTime)
				}
			}
			for _, ts := range tc.skipTimes {
				parsedTime, err := time.Parse(tfmt, ts)
				if err != nil {
					t.Fatal(err)
				}
				if j.ShouldRunAt(&parsedTime) {
					t.Fatalf("job %s should not run at %s", j.Name, parsedTime)
				}
			}
		}
	}
}
