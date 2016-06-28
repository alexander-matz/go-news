package main;

import (
    "time"
    "log"
    "flag"
    "os"
    "fmt"
    "strings"
    "sort"
    "html/template"
    "encoding/json"

    "github.com/gin-gonic/gin"
    )


const perPage int = 50
const dbFile string = "./data.bolt"
const configFile string = "./config.json"

func loadHTMLGlob(engine *gin.Engine, pattern string) {
    funcMap := template.FuncMap{
        "hashID": HashID,
        "dateFormat": func (t time.Time) string { return t.Format("2006-01-02 15:04:05") },
    }
    templ := template.Must(template.New("").Funcs(funcMap).ParseGlob(pattern))
    engine.SetHTMLTemplate(templ)
}

type config struct {
    BaseURL string  `json:"baseUrl,omitempty"`
    Addr    string  `json:"address,omitempty"`
}

func loadConfig(filename string) (*config, error) {
    file, err := os.Open(filename)
    if err != nil {
        return nil, err
    }
    defer file.Close()
    decoder := json.NewDecoder(file)
    res := &config{"", ":8080"}
    err = decoder.Decode(res)
    if err != nil {
        return nil, err
    }
    return res, nil
}

func cmdRun() error {

    var err error

    config, err := loadConfig(configFile)
    if err != nil {
        return err;
    }

    baseURL := config.BaseURL
    addr := config.Addr

    store, err := NewStore(dbFile)
    if err != nil {
        return err;
    }
    defer store.Close()

    feedd := NewFeedD(store)
    feedd.Start()
    defer feedd.Stop()

    articled := NewArticleD(store)
    articled.Start()
    defer articled.Stop()

    r := gin.Default()

    loadHTMLGlob(r, "./templates/*")

    r.Static(baseURL + "/static", "./static")

    r.GET(baseURL + "/", func(c *gin.Context) {
        c.HTML(200, "index.tmpl", gin.H{"base": baseURL})
    })

    r.GET(baseURL + "/f/", func(c *gin.Context) {
        posts := store.PostsAll(perPage)
        feedsMap := store.FeedsAllMap()
        c.HTML(200, "posts.tmpl", gin.H{"posts": posts, "feeds": feedsMap, "base": baseURL})
    })

    r.GET(baseURL + "/f/:feeds", func(c *gin.Context) {
        if c.Param("feeds") == "all" || c.Param("feeds") == "" {
            posts := store.PostsAll(perPage)
            feedsMap := store.FeedsAllMap()
            c.HTML(200, "posts.tmpl", gin.H{"posts": posts, "feeds": feedsMap, "base": baseURL})
        } else {
            feedlist := strings.Split(c.Param("feeds"), "+")
            posts := store.PostsByFeeds(perPage, feedlist)
            log.Printf("found %d posts", len(posts))
            feedsMap := store.FeedsAllMap()
            c.HTML(200, "posts.tmpl", gin.H{"posts": posts, "feeds": feedsMap, "base": baseURL})
        }
    })

    r.GET(baseURL + "/a/:articleid", func(c *gin.Context) {
        //postID, err := strconv.ParseInt(c.Param("articleid"), 10, 64)
        postID := UnhashID(c.Param("articleid"))
        if err != nil { c.String(200, err.Error()); return }
        post := store.PostsID(postID)
        if post == nil { c.String(200, fmt.Sprintf("invalid article: %d", postID)); return }
        content, err := articled.GetArticleContent(postID)
        if err != nil { c.String(200, err.Error()); return }
        c.HTML(200, "article.tmpl", gin.H{"title": post.Title, "content": template.HTML(content), "base": baseURL})
    });

    r.GET(baseURL + "/l/", func(c *gin.Context) {
        feeds := store.FeedsAll()
        c.HTML(200, "feeds.tmpl", gin.H{"feeds": feeds, "base": baseURL})
    })

    r.GET(baseURL + "/x/:articleid", func(c *gin.Context) {
        //postID, err := strconv.ParseInt(c.Param("articleid"), 10, 64)
        postID := UnhashID(c.Param("articleid"))
        if err != nil { c.String(200, err.Error()); return }
        post := store.PostsID(postID)
        if post == nil { c.String(200, fmt.Sprintf("invalid article: %d", postID)); return }
        content, err := articled.GetArticleContent(postID)
        if err != nil { c.String(200, err.Error()); return }
        c.String(200, content);
    });

    /*

    r.GET("/suggest", func(c *gin.Context) {
        c.String(http.StatusOK, "Not Implemented yet");
    })
    */

    r.Run(addr)

    return nil
}

type int64Slice []int64
func (s int64Slice) Len() int { return len(s) }
func (s int64Slice) Less(i, j int) bool { return s[i] < s[j] }
func (s int64Slice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func cmdTest() error {
    store, err := NewStore(dbFile)
    if err != nil {
        return err
    }
    defer store.Close()

    store.Dump()
    return nil
}

func cmdTestIDGen() error {
    n := 10
    m := 2000
    ids := make([]*IDGen, n)
    buf := make([]int64, n*m)
    for i := 0; i < n; i += 1 {
        ids[i] = NewIDGen(i+1)
    }
    for i := 0; i < n; i += 1 {
        for j := 0; j < m; j += 1 {
            buf[i*m + j] = ids[i].MakeID()
        }
    }
    sort.Sort(int64Slice(buf))
    duplicates := 0
    for i := 0; i < n-1; i += 1 {
        if buf[i] == buf[i+1] {
            duplicates += 1
        }
    }
    log.Printf("found %d duplicates", duplicates)

    return nil
}

func cmdTestFeeds() error {
    var err error

    store, err := NewStore(dbFile)
    if err != nil {
        return err
    }
    defer store.Close()

    feedd := NewFeedD(store)
    feedd.Start()
    defer feedd.Stop()

    time.Sleep(time.Minute * 10)

    return nil
}

func cmdInit() error {
    var err error
    os.Remove(dbFile)
    store, err := NewStore(dbFile)
    if err != nil { return err }
    defer store.Close()
    return nil
}

func cmdInitDebug() error {
    var err error

    os.Remove(dbFile)
    store, err := NewStore(dbFile)
    if err != nil {
        return err
    }
    defer store.Close()

    var feed Feed

    log.Printf("Adding feeds")
    feed.ID = MakeID()
    feed.URL = "http://feeds.bbci.co.uk/news/rss.xml"
    feed.Handle = "bbc"
    if err = store.FeedsSet(&feed); err != nil { return err }

    feed.ID = MakeID()
    feed.URL = "http://feeds.bbci.co.uk/news/world/europe/rss.xml"
    feed.Handle = "bbce"
    if err = store.FeedsSet(&feed); err != nil { return err }

    feed.ID = MakeID()
    feed.URL = "http://rss.csmonitor.com/feeds/csm"
    feed.Handle = "csm"
    if err = store.FeedsSet(&feed); err != nil { return err }

    return nil
}

func help() error {
    log.Printf("Usage:\n")
    log.Printf("    go-news command\n")
    log.Printf("\n")
    log.Printf("Commands are:\n")
    log.Printf("    run         run news web app normally\n")
    log.Printf("    init        (re-)initialize database\n")
    log.Printf("    initDbg     (re-)initialize database (fill with debug values)\n")
    log.Printf("    test        arbitray test code\n")
    log.Printf("    testFeeds   run rss daemon for a while\n")
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
    case cmdStr == "test":
        cmdFunc = cmdTest
    case cmdStr == "testFeeds":
        cmdFunc = cmdTestFeeds
    case true:
        cmdFunc = help
    }
    HashIDInit()
    if err := cmdFunc(); err != nil {
        log.Fatal(err.Error())
    }
}
