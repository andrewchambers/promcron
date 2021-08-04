package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type bounds struct {
	min, max uint
	names    map[string]uint
}

var (
	minuteBound = bounds{0, 59, nil}
	hourBound   = bounds{0, 23, nil}
	domBound    = bounds{1, 31, nil}
	monthBound  = bounds{1, 12, map[string]uint{
		"jan": 1,
		"feb": 2,
		"mar": 3,
		"apr": 4,
		"may": 5,
		"jun": 6,
		"jul": 7,
		"aug": 8,
		"sep": 9,
		"oct": 10,
		"nov": 11,
		"dec": 12,
	}}
	dowBound = bounds{0, 6, map[string]uint{
		"sun": 0,
		"mon": 1,
		"tue": 2,
		"wed": 3,
		"thu": 4,
		"fri": 5,
		"sat": 6,
	}}
)

const (
	// Set the top bit if a star was included in the expression.
	starBit = 1 << 63
)

func parseTimeField(field string, r bounds) (uint64, error) {
	var bits uint64
	ranges := strings.FieldsFunc(field, func(r rune) bool { return r == ',' })
	for _, expr := range ranges {
		bit, err := parseTimeRange(expr, r)
		if err != nil {
			return bits, err
		}
		bits |= bit
	}
	return bits, nil
}

func parseTimeRange(expr string, r bounds) (uint64, error) {
	var (
		start, end, step uint
		rangeAndStep     = strings.Split(expr, "/")
		lowAndHigh       = strings.Split(rangeAndStep[0], "-")
		singleDigit      = len(lowAndHigh) == 1
		err              error
	)

	var extra uint64
	if lowAndHigh[0] == "*" {
		start = r.min
		end = r.max
		extra = starBit
	} else {
		start, err = parseIntOrName(lowAndHigh[0], r.names)
		if err != nil {
			return 0, err
		}
		switch len(lowAndHigh) {
		case 1:
			end = start
		case 2:
			end, err = parseIntOrName(lowAndHigh[1], r.names)
			if err != nil {
				return 0, err
			}
		default:
			return 0, fmt.Errorf("too many hyphens: %s", expr)
		}
	}

	switch len(rangeAndStep) {
	case 1:
		step = 1
	case 2:
		step, err = mustParseInt(rangeAndStep[1])
		if err != nil {
			return 0, err
		}

		// Special handling: "N/step" means "N-max/step".
		if singleDigit {
			end = r.max
		}
		if step > 1 {
			extra = 0
		}
	default:
		return 0, fmt.Errorf("too many slashes: %s", expr)
	}

	if start < r.min {
		return 0, fmt.Errorf("beginning of range (%d) below minimum (%d): %s", start, r.min, expr)
	}
	if end > r.max {
		return 0, fmt.Errorf("end of range (%d) above maximum (%d): %s", end, r.max, expr)
	}
	if start > end {
		return 0, fmt.Errorf("beginning of range (%d) beyond end of range (%d): %s", start, end, expr)
	}
	if step == 0 {
		return 0, fmt.Errorf("step of range should be a positive number: %s", expr)
	}

	return getBits(start, end, step) | extra, nil
}

func parseIntOrName(expr string, names map[string]uint) (uint, error) {
	if names != nil {
		if namedInt, ok := names[strings.ToLower(expr)]; ok {
			return namedInt, nil
		}
	}
	return mustParseInt(expr)
}

func mustParseInt(expr string) (uint, error) {
	num, err := strconv.Atoi(expr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse int from %s", expr)
	}
	if num < 0 {
		return 0, fmt.Errorf("negative number (%d) not allowed: %s", num, expr)
	}

	return uint(num), nil
}

func getBits(min, max, step uint) uint64 {
	var bits uint64

	// If step is 1, use shifts.
	if step == 1 {
		return ^(math.MaxUint64 << (max + 1)) & (math.MaxUint64 << min)
	}

	// Else, use a simple loop.
	for i := min; i <= max; i += step {
		bits |= 1 << i
	}
	return bits
}

func ParseJobs(fname, tab string) ([]*Job, error) {
	jobs := []*Job{}
	lines := strings.Split(tab, "\n")
	for lno, l := range lines {

		parseError := func(err error) error {
			return fmt.Errorf("parse error %s:%d %s", fname, lno, err)
		}

		if strings.TrimSpace(l) == "" || l[0] == '#' {
			continue
		}

		// Split out our 7 fields
		curField := &strings.Builder{}
		fields := []string{}

		const ST_FIELD = 0
		const ST_WS = 1
		state := ST_WS
		for _, r := range l {
			switch state {
			case ST_FIELD:
				if len(fields) != 6 && (r == ' ' || r == '\t') {
					state = ST_WS
					fields = append(fields, curField.String())
					curField.Reset()
				} else {
					curField.WriteRune(r)
				}
			case ST_WS:
				if r != ' ' && r != '\t' {
					state = ST_FIELD
					curField.WriteRune(r)
				}
			}
		}
		fields = append(fields, curField.String())

		if len(fields) == 0 {
			continue
		}

		if len(fields) != 7 {
			return nil, parseError(fmt.Errorf("expected a label, timespec and a command"))
		}

		name := fields[0]
		minute, err := parseTimeField(fields[1], minuteBound)
		if err != nil {
			return nil, parseError(fmt.Errorf("invalid minute spec: %s", err))
		}
		hour, err := parseTimeField(fields[2], hourBound)
		if err != nil {
			return nil, parseError(fmt.Errorf("invalid hour spec: %s", err))
		}
		dom, err := parseTimeField(fields[3], domBound)
		if err != nil {
			return nil, parseError(fmt.Errorf("invalid day of month spec: %s", err))
		}
		month, err := parseTimeField(fields[4], monthBound)
		if err != nil {
			return nil, parseError(fmt.Errorf("invalid month spec: %s", err))
		}
		dow, err := parseTimeField(fields[5], dowBound)
		if err != nil {
			return nil, parseError(fmt.Errorf("invalid day of week spec: %s", err))
		}
		command := fields[6]

		jobs = append(jobs, &Job{
			Name:    name,
			Minute:  minute,
			Hour:    hour,
			Dom:     dom,
			Month:   month,
			Dow:     dow,
			Command: command,
		})
	}

	return jobs, nil
}
