package main;

import (
    "time"
    "github.com/gin-gonic/gin"
    _ "github.com/mauidude/go-readability"
    "log"
    "flag"
    "./store"
    "./feeds"
    )

type feed struct {
    Url, Handle string
}

var perPage = 50

func cmdRun() error {
    r := gin.Default()
    r.LoadHTMLGlob("templates/*")
    r.Static("/static", "./static")

    /*

    r.GET("/", func(c *gin.Context) {
        c.HTML(http.StatusOK, "index.tmpl", gin.H{"posts": PostsSorted[:perPage]})
    })


    r.GET("/c/:articlehash", func(c *gin.Context) {
        hash := c.Param("articlehash")
        if post, ok := PostsMap[hash]; ok {
            c.HTML(http.StatusOK, "article.tmpl", gin.H{"title": post.Title, "content": "No content supported yet"})
        } else {
            c.String(http.StatusBadRequest, fmt.Sprintf("Invalid article: %s", hash));
            return
        }
    });

    r.GET("/f/:feeds", func(c *gin.Context) {
        if (c.Param("feeds") == "all") {
            c.HTML(http.StatusOK, "index.tmpl", gin.H{"posts": PostsSorted[:perPage]});
        } else {
            feeds := strings.Split(c.Param("feeds"), "+")
            filter := make(map[string]bool)
            for _, v := range(feeds) {
                if _, ok := PostsMap[v]; !ok {
                    c.String(http.StatusBadRequest, fmt.Sprintf("Invalid feed: %s", v));
                    return
                }
                filter[v] = true
            }
            i := 0
            n := 0
            posts := make([]*Post, 0)
            for n < 50 {
                if (i >= len(PostsSorted)) { break; }
                post := PostsSorted[i]
                if _, ok := filter[post.Feed.Handle]; ok {
                    posts = append(posts, post)
                    n += 1
                }
                i += 1
            }
            c.HTML(http.StatusOK, "index.tmpl", gin.H{"posts": posts})
        }
    })

    r.GET("/feeds", func(c *gin.Context) {
        c.HTML(http.StatusOK, "feeds.tmpl", gin.H{"feeds": FeedsSorted})
    })

    r.GET("/suggest", func(c *gin.Context) {
        c.String(http.StatusOK, "Not Implemented yet");
    })
    */

    r.Run(":8080")

    return nil
}

func cmdTest() error {
    var num uint64
    var hash string
    num = 1; hash = store.Hash(num)
    log.Printf("num: %x, hashed: %s\n", num, hash)
    num = store.Unhash(hash)
    log.Printf("hash: %s, num: %x\n", hash, num)

    var err error

    if err = store.Init(); err != nil { return err }
    defer store.Deinit()

    if err = feeds.Init(); err != nil { return err }
    defer feeds.Deinit()

    time.Sleep(time.Minute * 30)

    return nil
}

func cmdInit() error {
    err := store.Reset()
    if err != nil { return err }
    return nil
}

func cmdInitDebug() error {
    err := store.Reset()
    if err != nil { return err }
    if err := store.Init(); err != nil { return err }
    defer store.Deinit()
    var feed store.Feed

    feed.Url = "http://feeds.bbci.co.uk/news/rss.xml"
    feed.Handle = "bbc"
    if err = store.FeedsAdd(&feed); err != nil { return err }

    feed.Url = "http://feeds.bbci.co.uk/news/world/europe/rss.xml"
    feed.Handle = "bbce"
    if err = store.FeedsAdd(&feed); err != nil { return err }

    feed.Url = "http://rss.csmonitor.com/feeds/csm"
    feed.Handle = "csm"
    if err = store.FeedsAdd(&feed); err != nil { return err }

    return nil
}

func cmdDumpDB() error {
    return store.Dump()
}

func help() error {
    log.Printf("Usage:\n")
    log.Printf("    go-news command\n")
    log.Printf("\n")
    log.Printf("Commands are:\n")
    log.Printf("    run     run news web app normally\n")
    log.Printf("    init    (re-)initialize database\n")
    log.Printf("    initDbg (re-)initialize database (fill with debug values)\n")
    log.Printf("    dumpDB  dump database\n")
    log.Printf("    test    arbitray test code\n")
    return nil
}

func main() {
    flag.Parse()
    if flag.NArg() < 1 {
        help()
        return
    }
    cmdStr := flag.Arg(0)
    var cmdFunc func() error
    switch {
    case cmdStr == "run":
        cmdFunc = cmdRun
    case cmdStr == "init":
        cmdFunc = cmdInit
    case cmdStr == "initDbg":
        cmdFunc = cmdInitDebug
    case cmdStr == "dumpDB":
        cmdFunc = cmdDumpDB
    case cmdStr == "test":
        cmdFunc = cmdTest
    case true:
        cmdFunc = help
    }
    if err := cmdFunc(); err != nil {
        log.Fatal(err.Error())
    }
}
