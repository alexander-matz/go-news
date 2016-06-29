package main;

import (
    "sync"
    "time"
    "github.com/speps/go-hashids"
    "strconv"
    _ "os"
    _ "log"
    )

func nowString() string {
    return time.Now().Format("15:04:05")
}

func DurationToHuman(d time.Duration) string {
    min := time.Minute
    hour := time.Hour
    day := hour * 24
    week := day * 7
    neg := false
    if (d < 0) {
        neg = true
        d = -d
    }
    _ = neg
    var tmp string
    switch {
    case d < min:
        tmp =  "<1 min"
    case d < 2*min:
        tmp =  "1 min"
    case d < hour:
        tmp =  strconv.Itoa(int(d/min)) + " min"
    case d < 2*hour:
        tmp =  "1 hour"
    case d < day:
        tmp =  strconv.Itoa(int(d/hour)) + " hours"
    case d < 2*day:
        tmp =  "1 day"
    case d < week:
        tmp =  strconv.Itoa(int(d/day)) + " days"
    case d < 2*week:
        tmp =  "1 week"
    default:
        tmp =  strconv.Itoa(int(d/week)) + "weeks"
    }
    return tmp
}

var hidd *hashids.HashIDData
var hid *hashids.HashID
func HashIDInit() {
    hidd = hashids.NewData()
    hidd.Salt = "phat news"
    hidd.MinLength = 5
    hid = hashids.NewWithData(hidd)
}

func HashID(val int64) string {
    hash, err := hid.EncodeInt64([]int64{val})
    if err != nil {
        return "#####"
    } else {
        return hash
    }
}

func UnhashID(hash string) int64 {
    nums, err := hid.DecodeInt64WithError(hash)
    if err != nil {
        return -1
    } else {
        return nums[0]
    }
}

/******************************************************************************
 * Twitter Snowflake unique ID generation
 * Machine ID is used for subsystems
 * [0  - 12) sequence
 * [12 - 22) subsystem
 * [22 - 63) timestamp
 * [63 - 64) 0
 * Also we allow custom timestamps because we want to sort posts by date of
 * publication not by date of discovery
 */

const MaxIDGen int = 1024

type IDGen struct {
    sub int
    seq int
}

func MakeIDRaw(t time.Time, machine int, sequence int) int64 {
    var ui uint64
    ts := uint64(t.UnixNano() / 1e6)
    ui = uint64(sequence & 0xfff) | uint64(machine & 0x3ff) << 12 | (ts & 0x1ffffffffff) << 22
    return int64(ui)
}

func NewIDGen(subsystem int) *IDGen {
    sub := int(subsystem & 0x3ff)
    seq := 0
    return &IDGen{sub, seq}
}

func (i *IDGen) MakeID() int64 {
    t := time.Now()
    i.seq = (i.seq + 1) % 0x1000
    return MakeIDRaw(t, i.sub, i.seq)
}

func (i *IDGen) MakeIDFromTimestamp(t time.Time) int64 {
    i.seq = (i.seq + 1) % 0x1000
    return MakeIDRaw(t, i.sub, i.seq)
}

// General purpose thread safe id generation for main subsystem

var idgen = NewIDGen(0)
var idmutex sync.Mutex
func MakeID() int64 {
    idmutex.Lock()
    defer idmutex.Unlock()
    return idgen.MakeID()
}
