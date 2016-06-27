package main;

import (
    "time"
    "github.com/speps/go-hashids"
    "os"
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

var seq uint16
var pid uint16

func InitId() {
    seq = 0
    pid = uint16(os.Getpid())
}

func makeTimestamp() int64 {
    return time.Now().UnixNano() / (int64(time.Millisecond)/int64(time.Nanosecond))
}

func MakeId() int64 {
    var result int64
    ts := makeTimestamp()
    result = 0
    // using twitter snowflake
    // [0  - 12) sequence
    // [12 - 22) pid
    // [22 - 63) timestamp (ms since epoch)
    result |= int64(ts & 0x1ffffffffff) << 22
    result |= int64(pid & 0x3ff) << 12
    result |= int64(seq & 0xfff)
    seq += 1
    return result
}
