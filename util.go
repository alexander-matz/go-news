package main;

import (
    "sync"
    "time"
    "github.com/speps/go-hashids"
    _ "os"
    _ "log"
    )

func nowString() string {
    return time.Now().Format("15:04:05")
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
 * Modified Twitter Snowflake unique ID generation
 * "Subsystem ID" instead of Machine ID
 * Order of Subsystem ID and Sequence is swtich for better mixing of results
 * [0  - 10) subsystem
 * [10 - 22) sequence
 * [22 - 63) timestamp
 * [63 - 64) 0
 * Also we allow custom timestamps because we want to sort posts by date of
 * publication not by date of discovery
 */

const MaxIDGen int = 1024

type IDGen struct {
    sub int64
    seq int
}

func NewIDGen(subsystem int) *IDGen {
    sub := int64(subsystem & 0x3ff)
    seq := 0
    return &IDGen{sub, seq}
}

func (i *IDGen) MakeID() int64 {
    var ui uint64
    ts := time.Now().UnixNano() / 1000
    ui = uint64(i.sub & 0x3ff) | (uint64(i.seq & 0xfff) << 10) | ((uint64(ts) & 0x1ffffffffff) << 22)
    i.seq += 1
    return int64(ui & 0x7fffffffffffffff)
}

func (i *IDGen) MakeIDFromTimestamp(t time.Time) int64 {
    var ui uint64
    ts := t.UnixNano() / 1000
    ui = uint64(i.sub & 0x3ff) | (uint64(i.seq & 0xfff) << 10) | ((uint64(ts) & 0x1ffffffffff) << 22)
    i.seq += 1
    return int64(ui & 0x7fffffffffffffff)
}

// General purpose thread safe id generation for main subsystem

var idgen = NewIDGen(0)
var idmutex sync.Mutex
func MakeID() int64 {
    idmutex.Lock()
    defer idmutex.Unlock()
    return idgen.MakeID()
}
