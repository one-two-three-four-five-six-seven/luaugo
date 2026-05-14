// Copyright (c) luaugo contributors. Licensed under the MIT License.
// Portions derived from Luau (https://github.com/luau-lang/luau),
// Copyright (c) 2019-2026 Roblox Corporation, MIT License.
// Portions derived from Lua 5.x, Copyright (c) 1994-2019 Lua.org, PUC-Rio, MIT License.

package lib

import (
	"fmt"
	"strconv"
	"time"

	"github.com/luaugo/luaugo/pkg/vm"
)

// startTime is the wall-clock instant at which the process started.
// os.clock returns the high-precision number of seconds since this
// point. Initialised in init() so the first call is meaningful.
var startTime time.Time

func init() {
	startTime = time.Now()
}

// openOSImpl registers the `os` library table as a global. Mirrors
// upstream loslib.cpp luaopen_os.
func openOSImpl(s *vm.State) {
	s.CreateTable(0, 4)
	s.LRegisterList([]vm.LFnEntry{
		{Name: "clock", Fn: osClock},
		{Name: "date", Fn: osDate},
		{Name: "difftime", Fn: osDifftime},
		{Name: "time", Fn: osTime},
	})
	// Publish as the global `os` and leave nothing on the stack to
	// match the convention used elsewhere in this package.
	s.PushValue(-1)
	s.SetGlobal("os")
	s.Pop(1)
}

// osClock returns seconds since process start.
func osClock(s *vm.State) int {
	s.PushNumber(time.Since(startTime).Seconds())
	return 1
}

// osDifftime returns a - b. b defaults to 0 to match upstream's
// luaL_optnumber default.
func osDifftime(s *vm.State) int {
	a := s.LCheckNumber(1)
	b := s.LOptNumber(2, 0)
	s.PushNumber(a - b)
	return 1
}

// osTime: no arg -> Unix seconds; table arg -> compose from
// {year,month,day,hour,min,sec} interpreted as UTC. Returning a UTC
// Unix timestamp matches upstream's os_timegm-based implementation.
func osTime(s *vm.State) int {
	if s.IsNoneOrNil(1) {
		s.PushNumber(float64(time.Now().Unix()))
		return 1
	}
	s.LCheckType(1, vm.TTable)
	s.SetTop(1)

	sec := getOptIntField(s, "sec", 0)
	minute := getOptIntField(s, "min", 0)
	hour := getOptIntField(s, "hour", 12)
	day := getReqIntField(s, "day")
	month := getReqIntField(s, "month")
	year := getReqIntField(s, "year")

	t := time.Date(year, time.Month(month), day, hour, minute, sec, 0, time.UTC)
	if t.Unix() < 0 {
		s.PushNil()
		return 1
	}
	s.PushNumber(float64(t.Unix()))
	return 1
}

// osDate implements both *t-table and strftime-format modes. Mirrors
// upstream loslib.cpp os_date.
func osDate(s *vm.State) int {
	format := s.LOptString(1, "%c")
	var t time.Time
	if s.IsNoneOrNil(2) {
		t = time.Now()
	} else {
		secs := s.LCheckNumber(2)
		t = time.Unix(int64(secs), 0)
	}

	utc := false
	if len(format) > 0 && format[0] == '!' {
		utc = true
		format = format[1:]
	}

	if utc {
		t = t.UTC()
	} else {
		// Upstream rejects pre-epoch local conversions because
		// Windows localtime() fails for them.
		if t.Unix() < 0 {
			s.PushNil()
			return 1
		}
		t = t.Local()
	}

	if format == "*t" {
		pushDateTable(s, t)
		return 1
	}

	s.PushString(strftime(format, t, s))
	return 1
}

// pushDateTable pushes a Lua table mirroring upstream's *t fields.
func pushDateTable(s *vm.State, t time.Time) {
	s.CreateTable(0, 9)
	s.PushInteger(int64(t.Second()))
	s.SetField(-2, "sec")
	s.PushInteger(int64(t.Minute()))
	s.SetField(-2, "min")
	s.PushInteger(int64(t.Hour()))
	s.SetField(-2, "hour")
	s.PushInteger(int64(t.Day()))
	s.SetField(-2, "day")
	s.PushInteger(int64(t.Month()))
	s.SetField(-2, "month")
	s.PushInteger(int64(t.Year()))
	s.SetField(-2, "year")
	// Lua wday: 1..7, Sunday=1. Go Weekday: 0..6, Sunday=0.
	s.PushInteger(int64(t.Weekday()) + 1)
	s.SetField(-2, "wday")
	// Lua yday: 1..366; Go YearDay: 1..366. Identical.
	s.PushInteger(int64(t.YearDay()))
	s.SetField(-2, "yday")
	// isdst: Go has no direct accessor. Approximate by comparing the
	// current zone offset against the year's January offset.
	_, off := t.Zone()
	jan := time.Date(t.Year(), time.January, 1, 0, 0, 0, 0, t.Location())
	_, janOff := jan.Zone()
	s.PushBoolean(off != janOff)
	s.SetField(-2, "isdst")
}

// getReqIntField returns the integer value at key in the table on top
// of the stack, raising if missing or not coercible to a number.
func getReqIntField(s *vm.State, key string) int {
	s.GetField(-1, key)
	defer s.Pop(1)
	if v, ok := s.ToInteger(-1); ok {
		return int(v)
	}
	if v, ok := s.ToNumber(-1); ok {
		return int(v)
	}
	s.LError("field '%s' missing in date table", key)
	return 0
}

// getOptIntField returns the integer value at key, or def if absent.
func getOptIntField(s *vm.State, key string, def int) int {
	s.GetField(-1, key)
	defer s.Pop(1)
	if s.IsNoneOrNil(-1) {
		return def
	}
	if v, ok := s.ToInteger(-1); ok {
		return int(v)
	}
	if v, ok := s.ToNumber(-1); ok {
		return int(v)
	}
	return def
}

// --- strftime ---------------------------------------------------------

var osShortDay = [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
var osLongDay = [...]string{
	"Sunday", "Monday", "Tuesday", "Wednesday",
	"Thursday", "Friday", "Saturday",
}
var osShortMonth = [...]string{
	"Jan", "Feb", "Mar", "Apr", "May", "Jun",
	"Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
}
var osLongMonth = [...]string{
	"January", "February", "March", "April", "May", "June",
	"July", "August", "September", "October", "November", "December",
}

// osStrftimeOptions mirrors LUA_STRFTIMEOPTIONS from loslib.cpp.
const osStrftimeOptions = "aAbBcdHIjmMpSUwWxXyYzZ%"

// strftime applies a small subset of POSIX strftime to t. It calls
// LArgError on an invalid specifier, mirroring upstream's argerror.
func strftime(fmtStr string, t time.Time, s *vm.State) string {
	out := make([]byte, 0, len(fmtStr)+16)
	for i := 0; i < len(fmtStr); i++ {
		c := fmtStr[i]
		if c != '%' || i+1 == len(fmtStr) {
			out = append(out, c)
			continue
		}
		i++
		spec := fmtStr[i]
		if !osContainsByte(osStrftimeOptions, spec) {
			s.LArgError(1, "invalid conversion specifier")
			return ""
		}
		out = append(out, formatStrftimeSpec(spec, t)...)
	}
	return string(out)
}

func osContainsByte(s string, b byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return true
		}
	}
	return false
}

// pad2 returns the two-digit zero-padded decimal of n (expected 0..99).
func osPad2(n int) string {
	if n >= 0 && n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// pad3 returns the three-digit zero-padded decimal of n.
func osPad3(n int) string {
	switch {
	case n < 10:
		return "00" + strconv.Itoa(n)
	case n < 100:
		return "0" + strconv.Itoa(n)
	default:
		return strconv.Itoa(n)
	}
}

// weekNumber returns the week-of-year using the start-of-week day
// (0=Sunday for %U, 1=Monday for %W).
func osWeekNumber(t time.Time, startWday int) int {
	yday := t.YearDay() - 1 // 0-based
	wday := int(t.Weekday())
	diff := (wday - startWday + 7) % 7
	return (yday - diff + 7) / 7
}

// formatStrftimeSpec returns the textual replacement for a single
// strftime specifier byte (without the leading '%').
func formatStrftimeSpec(spec byte, t time.Time) string {
	switch spec {
	case 'a':
		return osShortDay[int(t.Weekday())]
	case 'A':
		return osLongDay[int(t.Weekday())]
	case 'b':
		return osShortMonth[int(t.Month())-1]
	case 'B':
		return osLongMonth[int(t.Month())-1]
	case 'c':
		return fmt.Sprintf("%s %s %2d %02d:%02d:%02d %d",
			osShortDay[int(t.Weekday())],
			osShortMonth[int(t.Month())-1],
			t.Day(), t.Hour(), t.Minute(), t.Second(), t.Year())
	case 'd':
		return osPad2(t.Day())
	case 'H':
		return osPad2(t.Hour())
	case 'I':
		h := t.Hour() % 12
		if h == 0 {
			h = 12
		}
		return osPad2(h)
	case 'j':
		return osPad3(t.YearDay())
	case 'm':
		return osPad2(int(t.Month()))
	case 'M':
		return osPad2(t.Minute())
	case 'p':
		if t.Hour() < 12 {
			return "AM"
		}
		return "PM"
	case 'S':
		return osPad2(t.Second())
	case 'U':
		return osPad2(osWeekNumber(t, 0))
	case 'w':
		return strconv.Itoa(int(t.Weekday()))
	case 'W':
		return osPad2(osWeekNumber(t, 1))
	case 'x':
		return fmt.Sprintf("%02d/%02d/%02d", int(t.Month()), t.Day(), t.Year()%100)
	case 'X':
		return fmt.Sprintf("%02d:%02d:%02d", t.Hour(), t.Minute(), t.Second())
	case 'y':
		return osPad2(t.Year() % 100)
	case 'Y':
		return strconv.Itoa(t.Year())
	case 'z':
		_, off := t.Zone()
		sign := byte('+')
		if off < 0 {
			sign = '-'
			off = -off
		}
		hh := off / 3600
		mm := (off % 3600) / 60
		return fmt.Sprintf("%c%02d%02d", sign, hh, mm)
	case 'Z':
		name, _ := t.Zone()
		return name
	case '%':
		return "%"
	}
	return ""
}
