package main;

import (
    "time"
    "sync"
    "log"
    )

type Stats struct {
    VisitorsToday           int
    VisitorsPerHour         float32
    PostsPerHourTotal       float32
    PostsPerHourByFeed      map[int64]float32
    FeedQueriesPerHour      map[int64]float32
    ArticlesPerHourByFeed   map[int64]float32
    ArticlesReadPerHourByFeed map[int64]float32

    log     *log.Logger

    mutex   sync.Mutex
    stop    chan bool
    active  bool
}

func NewStats(log *log.Logger) *Stats {
    s := Stats{}
    s.PostsPerHourByFeed = make(map[int64]float32)
    s.FeedQueriesPerHour = make(map[int64]float32)
    s.ArticlesPerHourByFeed = make(map[int64]float32)
    s.stop = make(chan bool)
    s.log = log
    return &s
}

func (s *Stats) Start() {
    if s.active {
        return
    }
    go func() {
        delay := time.Minute * 1
        for true {
            select {
            case <-s.stop:
                return
            case <-time.After(delay):
            }
            s.log.Printf("updating stats")
        }
    }()
    s.active = true
}

func (s *Stats) Stop() {
    s.stop <- true
    s.active = false
}

func (s *Stats) Tick() {
}
