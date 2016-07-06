package main

import (
	"log"
	"sync"
	"time"
)

type Stats struct {
	VisitsPerHour      float32
	VisitorsPerHour    float32
	PostsPerHour       float32
	PostsReadPerHour   float32
	ReadabilityPerHour float32

	visitors    map[string]int
	visits      int
	posts       int
	read        int
	readability int

	fraction float32

	log *log.Logger

	mutex  sync.Mutex
	stop   chan bool
	active bool
}

func NewStats(log *log.Logger) *Stats {
	s := Stats{}
	s.stop = make(chan bool)
	s.log = log
	s.fraction = 10 / 60.0
	return &s
}

func (s *Stats) Start() {
	if s.active {
		return
	}
	go func() {
		delay := time.Duration(float32(time.Hour) * s.fraction)
		for true {
			select {
			case <-s.stop:
				return
			case <-time.After(delay):
				s.tick()
			}
			s.log.Printf("updating")
		}
	}()
	s.active = true
}

func (s *Stats) AddVisit(addr string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	num, ok := s.visitors[addr]
	if !ok {
		s.visitors[addr] = 1
	} else {
		s.visitors[addr] = num + 1
	}
	s.visits += 1
}

func (s *Stats) AddPosts(num int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.posts += num
}

func (s *Stats) AddRead(num int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.read += num
}

func (s *Stats) AddReadability(num int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.readability += num
}

func (s *Stats) Stop() {
	s.stop <- true
	s.active = false
}

// oldAvg       Current average
// incr         Partial sample (e.g. average visitors over an hour, this is
//              visitors in the last 5 minutes)
// fraction     The fraction of partial/full, in the example above 5/60
// weight       Influence of sample over a full period
func slidingAdd(oldAvg, incr, fraction, weight float32) float32 {
	actWeight := weight * fraction
	sample := incr / fraction
	return oldAvg*(1-actWeight) + sample*actWeight
}

func (s *Stats) tick() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.VisitsPerHour = slidingAdd(s.VisitsPerHour, float32(s.visits), s.fraction, 0.05)
	s.visits = 0

	s.VisitorsPerHour = slidingAdd(s.VisitorsPerHour, float32(len(s.visitors)), s.fraction, 0.05)
	s.visitors = make(map[string]int)

	s.PostsPerHour = slidingAdd(s.PostsPerHour, float32(s.posts), s.fraction, 0.01)
	s.posts = 0

	s.PostsReadPerHour = slidingAdd(s.PostsReadPerHour, float32(s.read), s.fraction, 0.05)
	s.read = 0

	s.ReadabilityPerHour = slidingAdd(s.ReadabilityPerHour, float32(s.readability), s.fraction, 0.05)
	s.readability = 0
}
