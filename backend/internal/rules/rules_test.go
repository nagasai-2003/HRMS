package rules

import (
	"net/netip"
	"testing"
	"time"
)

var hq = Office{Name: "HQ Hyderabad", Lat: 17.4483, Lng: 78.3915, RadiusM: 150}

func officeInput(loc *Location, ip string, rule string) Input {
	return Input{
		Mode:        ModeOffice,
		Location:    loc,
		IP:          netip.MustParseAddr(ip),
		Offices:     []Office{hq},
		Networks:    []netip.Prefix{netip.MustParsePrefix("49.205.112.18/32"), netip.MustParsePrefix("183.82.96.0/28")},
		Rule:        rule,
		MaxAccuracy: 100,
	}
}

var (
	inside   = &Location{Lat: 17.4486, Lng: 78.3917, AccuracyM: 25}  // ~40 m from HQ
	outside  = &Location{Lat: 17.4650, Lng: 78.3915, AccuracyM: 25}  // ~1.9 km away
	blurry   = &Location{Lat: 17.4486, Lng: 78.3917, AccuracyM: 900} // inside but vague
	officeIP = "49.205.112.18"
	rangeIP  = "183.82.96.7"
	homeIP   = "157.48.33.1"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		name  string
		in    Input
		allow bool
		code  string
	}{
		{"office: inside on office Wi-Fi", officeInput(inside, officeIP, "both"), true, ""},
		{"office: IP from an allowed range", officeInput(inside, rangeIP, "both"), true, ""},
		{"office: IPv4-mapped IPv6 address", officeInput(inside, "::ffff:49.205.112.18", "both"), true, ""},
		{"office: inside on mobile hotspot", officeInput(inside, homeIP, "both"), false, CodeWrongNetwork},
		{"office: outside on office IP (VPN)", officeInput(outside, officeIP, "both"), false, CodeOutside},
		{"office: outside and wrong network", officeInput(outside, homeIP, "both"), false, CodeOutsideAndNet},
		{"office: location off", officeInput(nil, officeIP, "both"), false, CodeLocationOff},
		{"office: blurry location, good network", officeInput(blurry, officeIP, "both"), false, CodeLowAccuracy},
		{"office: blurry location, bad network", officeInput(blurry, homeIP, "both"), false, CodeLowAccuracy},
		{"either: inside, wrong network", officeInput(inside, homeIP, "either"), true, ""},
		{"either: outside, office network", officeInput(outside, officeIP, "either"), true, ""},
		{"either: blurry, office network", officeInput(blurry, officeIP, "either"), true, ""},
		{"either: outside and wrong network", officeInput(outside, homeIP, "either"), false, CodeOutsideAndNet},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Decide(c.in)
			if r.Allowed != c.allow || r.Code != c.code {
				t.Fatalf("got allowed=%v code=%q (%s), want allowed=%v code=%q", r.Allowed, r.Code, r.Message, c.allow, c.code)
			}
		})
	}
}

func TestDecideRemoteDays(t *testing.T) {
	for _, m := range []Mode{ModeWFH, ModeOnDuty, ModeHybridRemote, ModeRemote} {
		in := officeInput(outside, homeIP, "both")
		in.Mode = m
		if r := Decide(in); !r.Allowed || r.NetworkCheck {
			t.Errorf("%s from home should pass without a network check: %+v", m, r)
		}
		in.Location = nil
		if r := Decide(in); r.Allowed || r.Code != CodeLocationOff {
			t.Errorf("%s without location should be blocked: %+v", m, r)
		}
	}
}

func TestDecideHolidayAndNoOffice(t *testing.T) {
	in := officeInput(inside, officeIP, "both")
	in.Mode, in.HolidayName = ModeHoliday, "Diwali"
	if r := Decide(in); r.Allowed || r.Code != CodeHoliday {
		t.Fatalf("holiday: %+v", r)
	}
	in = officeInput(inside, officeIP, "both")
	in.Offices = nil
	if r := Decide(in); r.Allowed || r.Code != CodeNoOffices {
		t.Fatalf("no offices: %+v", r)
	}
}

func TestNearestOffice(t *testing.T) {
	vizag := Office{Name: "Branch Vizag", Lat: 17.7231, Lng: 83.3013, RadiusM: 120}
	in := officeInput(&Location{Lat: 17.7232, Lng: 83.3014, AccuracyM: 10}, officeIP, "both")
	in.Offices = []Office{hq, vizag}
	r := Decide(in)
	if !r.Allowed || r.Office.Name != "Branch Vizag" {
		t.Fatalf("should match Vizag: %+v", r)
	}
}

func TestModeFor(t *testing.T) {
	wed := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC) // a Wednesday
	thu := wed.AddDate(0, 0, 1)
	cases := []struct {
		name string
		in   ModeInput
		want Mode
	}{
		{"office policy", ModeInput{Policy: "office", Day: wed}, ModeOffice},
		{"hybrid on remote weekday", ModeInput{Policy: "hybrid", RemoteWeekdays: []int16{3}, Day: wed}, ModeHybridRemote},
		{"hybrid on office weekday", ModeInput{Policy: "hybrid", RemoteWeekdays: []int16{3}, Day: thu}, ModeOffice},
		{"remote policy", ModeInput{Policy: "remote", Day: thu}, ModeRemote},
		{"approved WFH", ModeInput{Policy: "office", Day: thu, ApprovedKind: "wfh"}, ModeWFH},
		{"approved on-duty", ModeInput{Policy: "office", Day: thu, ApprovedKind: "on_duty"}, ModeOnDuty},
		{"holiday", ModeInput{Policy: "remote", Day: thu, HolidayName: "Diwali"}, ModeHoliday},
		{"on-duty on a holiday", ModeInput{Policy: "office", Day: thu, HolidayName: "Diwali", ApprovedKind: "on_duty"}, ModeOnDuty},
	}
	for _, c := range cases {
		if got := ModeFor(c.in); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestSummarise(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 11, 13, h, m, 0, 0, time.UTC) }
	ev := []Event{
		{Kind: ActCheckIn, At: at(9, 0), Mode: "office"},
		{Kind: ActBreakStart, At: at(13, 0), Reason: "Lunch"},
		{Kind: ActBreakEnd, At: at(13, 45)},
		{Kind: ActBreakEnd, At: at(13, 50)}, // duplicate: skipped
		{Kind: ActCheckOut, At: at(18, 0)},
	}
	s := Summarise(ev, at(20, 0))
	if s.Phase != PhaseDone || s.WorkedMin != 8*60+15 || s.BreakMin != 45 || len(s.Breaks) != 1 || s.Mode != "office" {
		t.Fatalf("full day: %+v", s)
	}

	// Still on a break: the open break counts up to now.
	s = Summarise(ev[:2], at(13, 20))
	if s.Phase != PhaseBreak || s.WorkedMin != 240 || s.BreakMin != 20 || s.Breaks[0].Minutes != 20 {
		t.Fatalf("open break: %+v", s)
	}

	// Check out during a break closes the break; a second session adds up.
	ev2 := []Event{
		{Kind: ActCheckIn, At: at(9, 0)},
		{Kind: ActBreakStart, At: at(12, 0), Reason: "Personal"},
		{Kind: ActCheckOut, At: at(12, 30)},
		{Kind: ActCheckIn, At: at(14, 0)},
	}
	s = Summarise(ev2, at(15, 0))
	if s.Phase != PhaseIn || s.WorkedMin != 240 || s.BreakMin != 30 || s.Breaks[0].End == nil {
		t.Fatalf("second session: %+v", s)
	}
}

func TestCalendar(t *testing.T) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	// 20:00 UTC on the 12th is 01:30 on the 13th in India.
	d := DayOf(time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC), ist)
	if d.Day() != 13 {
		t.Fatalf("DayOf: %v", d)
	}
	if ws := WeekStart(d); ws.Day() != 9 || ws.Weekday() != time.Monday {
		t.Fatalf("WeekStart: %v", ws)
	}
	sun := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	if ws := WeekStart(sun); ws.Day() != 9 {
		t.Fatalf("WeekStart of Sunday: %v", ws)
	}
	from, to := MonthRange(d)
	if from.Day() != 1 || to.Day() != 30 {
		t.Fatalf("MonthRange: %v %v", from, to)
	}
}
