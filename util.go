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
func HashIdInit() {
    hidd = hashids.NewData()
    hidd.Salt = "phat news"
    hidd.MinLength = 5
    hid = hashids.NewWithData(hidd)
}

func HashId(val int64) string {
    hash, err := hid.EncodeInt64([]int64{val})
    if err != nil {
        return "#####"
    } else {
        return hash
    }
}

func UnhashId(hash string) int64 {
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

const MaxIdGen int = 1024

type IdGen struct {
    sub int64
    seq int
}

func timestamp() int64 {
    return time.Now().UnixNano() / (int64(time.Millisecond)/int64(time.Nanosecond))
}

func NewIdGen(subsystem int) *IdGen {
    sub := int64(subsystem & 0x3ff)
    seq := 0
    return &IdGen{sub, seq}
}

func (i *IdGen) MakeId() int64 {
    var result int64
    ts := timestamp()
    result = 0
    result |= int64(ts & 0x1ffffffffff) << 22
    result |= int64(i.seq & 0xfff) << 10
    result |= int64(i.sub & 0x3ff)
    i.seq += 1
    if i.seq >= 1024 {
        time.Sleep(time.Millisecond)
        i.seq = 0
    }
    return result
}

func (i *IdGen) MakeIdFromTimestamp(t time.Time) int64 {
    var result int64
    ts := t.UnixNano() / (int64(time.Millisecond)/int64(time.Nanosecond))
    result = 0
    result |= int64(ts & 0x1ffffffffff) << 22
    result |= int64(i.seq & 0xfff) << 10
    result |= int64(i.sub & 0x3ff)
    i.seq += 1
    if i.seq >= 1024 {
        time.Sleep(time.Millisecond)
        i.seq = 0
    }
    return result
}

// General purpose thread safe id generation for main subsystem

var idgen = NewIdGen(0)
var idmutex sync.Mutex
func MakeId() int64 {
    idmutex.Lock()
    defer idmutex.Unlock()
    return idgen.MakeId()
}
