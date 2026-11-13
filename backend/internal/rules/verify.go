// Package rules holds the attendance decisions as plain functions, so every
// situation (office day, WFH, hybrid, on-duty, wrong network, location off)
// can be tested without a database.
package rules

import (
	"math"
	"net/netip"
	"strconv"

	"github.com/google/uuid"
)

// Mode is why a person is allowed to work from where they are today.
type Mode string

const (
	ModeOffice       Mode = "office"        // office day: location and network are checked
	ModeWFH          Mode = "wfh"           // approved WFH request
	ModeOnDuty       Mode = "on_duty"       // approved on-duty (client site, travel)
	ModeHybridRemote Mode = "hybrid_remote" // hybrid policy, remote weekday
	ModeRemote       Mode = "remote"        // fully remote policy
	ModeHoliday      Mode = "holiday"
)

// Remote reports whether the day skips the office location and network checks.
func (m Mode) Remote() bool {
	return m == ModeWFH || m == ModeOnDuty || m == ModeHybridRemote || m == ModeRemote
}

// Block codes shown to the person and logged for HR.
const (
	CodeHoliday       = "holiday"
	CodeLocationOff   = "location_off"
	CodeLowAccuracy   = "low_accuracy"
	CodeOutside       = "outside_office"
	CodeWrongNetwork  = "wrong_network"
	CodeOutsideAndNet = "outside_and_network"
	CodeNoOffices     = "no_offices"
)

type Office struct {
	ID      uuid.UUID
	Name    string
	Lat     float64
	Lng     float64
	RadiusM float64
}

type Location struct {
	Lat       float64
	Lng       float64
	AccuracyM float64
}

type Input struct {
	Mode        Mode
	HolidayName string
	Location    *Location // nil when the browser gave no location
	IP          netip.Addr
	Offices     []Office
	Networks    []netip.Prefix
	Rule        string  // "both" or "either"
	MaxAccuracy float64 // metres
}

type Result struct {
	Allowed bool
	Code    string
	Message string

	// What was checked, for the record and for the screen.
	Office       *Office
	DistanceM    *float64
	InFence      bool
	AccuracyOK   bool
	NetworkOK    bool
	NetworkCheck bool // false on remote days
	FenceCheck   bool
}

// Decide runs the checks in a fixed order:
// holiday → remote day (location only) → location on → accuracy and
// geofence → office network → the both/either rule.
func Decide(in Input) Result {
	r := Result{}
	if in.Mode == ModeHoliday {
		r.Code = CodeHoliday
		r.Message = "Today is a holiday (" + in.HolidayName + "). Ask HR for on-duty if you need to work."
		return r
	}
	if in.Location == nil {
		r.Code = CodeLocationOff
		r.Message = "Turn on location for this site. Attendance needs your location every time."
		return r
	}
	if in.Mode.Remote() {
		// Location is recorded but the office rules do not apply.
		r.Allowed = true
		r.AccuracyOK = true
		return r
	}

	r.FenceCheck = true
	r.NetworkCheck = true
	r.AccuracyOK = in.Location.AccuracyM <= in.MaxAccuracy
	r.NetworkOK = ipAllowed(in.IP, in.Networks)

	if len(in.Offices) == 0 {
		r.Code = CodeNoOffices
		r.Message = "No office location is set up yet. Ask an admin to add one."
		return r
	}
	off, dist := nearest(in.Offices, in.Location.Lat, in.Location.Lng)
	r.Office = &off
	r.DistanceM = &dist
	// The reading is only trusted inside the fence if it is accurate enough.
	r.InFence = r.AccuracyOK && dist <= off.RadiusM

	pass := r.InFence && r.NetworkOK
	if in.Rule == "either" {
		pass = r.InFence || r.NetworkOK
	}
	if pass {
		r.Allowed = true
		return r
	}

	switch {
	case !r.AccuracyOK && !r.NetworkOK:
		r.Code = CodeLowAccuracy
		r.Message = "Your location is not accurate enough (" + metres(in.Location.AccuracyM) + "). Move near a window or turn on Wi-Fi, then try again."
	case !r.InFence && !r.NetworkOK:
		r.Code = CodeOutsideAndNet
		r.Message = "You are " + metres(dist) + " from " + off.Name + " and not on the office network."
	case !r.AccuracyOK:
		r.Code = CodeLowAccuracy
		r.Message = "Your location is not accurate enough (" + metres(in.Location.AccuracyM) + "). Move near a window or turn on Wi-Fi, then try again."
	case !r.InFence:
		r.Code = CodeOutside
		r.Message = "You are " + metres(dist) + " from " + off.Name + ". You need to be inside the office for this."
	default:
		r.Code = CodeWrongNetwork
		r.Message = "You are not on the office network (" + in.IP.String() + "). Connect to the office Wi-Fi and turn off any VPN."
	}
	return r
}

func ipAllowed(ip netip.Addr, nets []netip.Prefix) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.Unmap()
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func nearest(offices []Office, lat, lng float64) (Office, float64) {
	best, bestD := offices[0], math.Inf(1)
	for _, o := range offices {
		if d := Haversine(lat, lng, o.Lat, o.Lng); d < bestD {
			best, bestD = o, d
		}
	}
	return best, bestD
}

// Haversine returns the distance in metres between two points.
func Haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const earth = 6371000.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * earth * math.Asin(math.Min(1, math.Sqrt(a)))
}

func metres(m float64) string {
	if m >= 1000 {
		return strconv.FormatFloat(math.Round(m/100)/10, 'f', -1, 64) + " km"
	}
	return strconv.Itoa(int(math.Round(m))) + " m"
}
