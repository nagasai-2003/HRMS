package rules

import (
	"slices"
	"time"
)

// Phase is where a person is in their day.
type Phase string

const (
	PhaseOut   Phase = "out"   // not checked in yet
	PhaseIn    Phase = "in"    // working
	PhaseBreak Phase = "break" // on a break
	PhaseDone  Phase = "done"  // checked out (can check in again)
)

const (
	ActCheckIn    = "check_in"
	ActBreakStart = "break_start"
	ActBreakEnd   = "break_end"
	ActCheckOut   = "check_out"
)

// Allowed reports whether an action makes sense from a phase.
func Allowed(p Phase, action string) bool {
	switch p {
	case PhaseOut, PhaseDone:
		return action == ActCheckIn
	case PhaseIn:
		return action == ActBreakStart || action == ActCheckOut
	case PhaseBreak:
		return action == ActBreakEnd || action == ActCheckOut
	}
	return false
}

type Event struct {
	Kind   string
	At     time.Time
	Mode   string
	Reason string
	Auto   bool
}

type Break struct {
	Reason  string     `json:"reason"`
	Start   time.Time  `json:"start"`
	End     *time.Time `json:"end"`
	Minutes int        `json:"minutes"`
}

type DaySummary struct {
	Phase      Phase      `json:"phase"`
	Mode       string     `json:"mode"`
	FirstIn    *time.Time `json:"first_in"`
	LastOut    *time.Time `json:"last_out"`
	WorkedMin  int        `json:"worked_min"`
	BreakMin   int        `json:"break_min"`
	Breaks     []Break    `json:"breaks"`
	AutoClosed bool       `json:"auto_closed"`
}

// Summarise walks a day's events (sorted by time). Open work or breaks count
// up to now. Events that do not fit the current phase are skipped.
func Summarise(events []Event, now time.Time) DaySummary {
	s := DaySummary{Phase: PhaseOut, Breaks: []Break{}}
	var workFrom, breakFrom time.Time
	var worked, onBreak time.Duration
	for _, e := range events {
		if !Allowed(s.Phase, e.Kind) {
			continue
		}
		switch e.Kind {
		case ActCheckIn:
			s.Phase = PhaseIn
			workFrom = e.At
			if s.FirstIn == nil {
				t := e.At
				s.FirstIn = &t
				s.Mode = e.Mode
			}
		case ActBreakStart:
			worked += e.At.Sub(workFrom)
			s.Phase = PhaseBreak
			breakFrom = e.At
			s.Breaks = append(s.Breaks, Break{Reason: e.Reason, Start: e.At})
		case ActBreakEnd:
			onBreak += e.At.Sub(breakFrom)
			closeBreak(&s, e.At)
			s.Phase = PhaseIn
			workFrom = e.At
		case ActCheckOut:
			if s.Phase == PhaseIn {
				worked += e.At.Sub(workFrom)
			} else {
				onBreak += e.At.Sub(breakFrom)
				closeBreak(&s, e.At)
			}
			s.Phase = PhaseDone
			t := e.At
			s.LastOut = &t
			s.AutoClosed = e.Auto
		}
	}
	switch s.Phase {
	case PhaseIn:
		if now.After(workFrom) {
			worked += now.Sub(workFrom)
		}
	case PhaseBreak:
		if now.After(breakFrom) {
			d := now.Sub(breakFrom)
			onBreak += d
			s.Breaks[len(s.Breaks)-1].Minutes = int(d.Minutes())
		}
	}
	s.WorkedMin = int(worked.Minutes())
	s.BreakMin = int(onBreak.Minutes())
	return s
}

func closeBreak(s *DaySummary, at time.Time) {
	b := &s.Breaks[len(s.Breaks)-1]
	t := at
	b.End = &t
	b.Minutes = int(at.Sub(b.Start).Minutes())
}

// Calendar helpers. A "day" is a company-local date stored as midnight UTC,
// which is how Postgres date columns come back.

func DayOf(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
}

// At turns a company-local day and a clock time (minutes after midnight) into an instant.
func At(day time.Time, minutes int, loc *time.Location) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), minutes/60, minutes%60, 0, 0, loc)
}

func WeekStart(day time.Time) time.Time {
	wd := int(day.Weekday())
	if wd == 0 {
		wd = 7
	}
	return day.AddDate(0, 0, 1-wd)
}

func MonthRange(day time.Time) (time.Time, time.Time) {
	first := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
	return first, first.AddDate(0, 1, -1)
}

// ISOWeekday is 1 (Monday) to 7 (Sunday).
func ISOWeekday(day time.Time) int16 {
	wd := int16(day.Weekday())
	if wd == 0 {
		return 7
	}
	return wd
}

type ModeInput struct {
	Policy         string // office, hybrid, remote
	RemoteWeekdays []int16
	Day            time.Time
	ApprovedKind   string // "wfh", "on_duty" or ""
	HolidayName    string // "" when it is not a holiday
}

// ModeFor picks today's mode. Approved requests win, then holidays, then the
// person's work policy.
func ModeFor(in ModeInput) Mode {
	switch in.ApprovedKind {
	case "on_duty":
		return ModeOnDuty
	case "wfh":
		return ModeWFH
	}
	if in.HolidayName != "" {
		return ModeHoliday
	}
	switch in.Policy {
	case "remote":
		return ModeRemote
	case "hybrid":
		if slices.Contains(in.RemoteWeekdays, ISOWeekday(in.Day)) {
			return ModeHybridRemote
		}
	}
	return ModeOffice
}
