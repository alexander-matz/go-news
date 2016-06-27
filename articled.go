package main;

import (
    "time"
    )

type Content struct {
    Id          int64
    ArticleId   int64
    Content     string
    LastAccess  time.Time
}

type ArticleD struct {
    contentMap  map[int64]*Content
    articleMap  map[int64]*Content
    stop        chan bool
    active      bool
    store       *Store
}

func NewArticleD(store *Store) *ArticleD{
    res := &ArticleD{make(map[int64]*Content), make(map[int64]*Content),
                    make(chan bool, 1), false, store}
    return res
}

func (d *ArticleD) Start() {
    if !d.active {
        d.active = true
        go d.run()
    }
}

func (d *ArticleD) Stop() {
    if d.active {
        d.stop <- true
        d.active = false
    }
}

func (d *ArticleD) run() {
}
