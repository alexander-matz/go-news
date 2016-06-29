package main;

import (
    "time"
    "log"
    "flag"
    "os"
    "fmt"
    "strings"
    "html/template"
    "encoding/json"
    "encoding/hex"
    "errors"
    "crypto/sha1"
    "bytes"

    "golang.org/x/crypto/ssh/terminal"
    "github.com/gin-gonic/gin"
    )


const perPage int = 50
const dbFile string = "./data.bolt"
const configFile string = "./config.json"

func loadHTMLGlob(engine *gin.Engine, pattern string) {
    funcMap := template.FuncMap{
        "hashID": HashID,
        "date": func (t time.Time) string {
            return t.Format("2006-01-02 15:04 -0700")
        },
        "when": func (t time.Time) string {
            return DurationToHuman(t.UTC().Sub(time.Now().UTC()))
        },
    }
    templ := template.Must(template.New("").Funcs(funcMap).ParseGlob(pattern))
    engine.SetHTMLTemplate(templ)
}

type config struct {
    BaseURL string  `json:"baseUrl,omitempty"`
    Addr    string  `json:"address,omitempty"`
    HashHex string  `json:"passwd,omitempty"`
}

func loadConfig(filename string) (*config, error) {
    file, err := os.Open(filename)
    if err != nil {
        return nil, err
    }
    defer file.Close()
    decoder := json.NewDecoder(file)
    res := &config{"", ":8080", ""}
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
    if config.HashHex == "" {
        return errors.New("NO PASSWORD HASH SPECIFIED")
    }

    baseURL := config.BaseURL
    addr := config.Addr
    hash, err := hex.DecodeString(config.HashHex)
    if err != nil {
        return errors.New("invalid hex string for password hash")
    }

    /* START SUBSYSTEMS */

    store, err := NewStore(dbFile)
    if err != nil {
        return err;
    }
    defer store.Close()

    feedd := NewFeedD(store)
    feedd.Start()
    defer feedd.Stop()

    /* PERIODICALLY TRIM ARTICLES */
    stoptrim := make(chan bool, 1)
    go func (stop chan bool) {
        for true {
            select {
            case <-stop:
                return;
            case <-time.After(time.Minute * 60):
                break;
            }
            store.PostsTrim()
        }
    }(stoptrim)
    defer func() {
        stoptrim <- true
    }()

    /* CONFIGURE SERVER */
    r := gin.Default()

    loadHTMLGlob(r, "./templates/*")

    r.Static(baseURL + "/static", "./static")

    /*   /   - INDEX */

    r.GET(baseURL + "/", func(c *gin.Context) {
        sitemap := make(map[string]string)
        sitemap["/f/"] = "show all feeds"
        sitemap["/f/bbc+bbce"] = "show only feeds BBC + BBC Europe"
        sitemap["/l/"] = "list available feeds"
        sitemap["/s/"] = "suggest a feed to add"
        c.HTML(200, "index.tmpl", gin.H{"base": baseURL, "sitemap": sitemap})
    })



    /*   /f/ - NEWS */

    r.GET(baseURL + "/f/", func(c *gin.Context) {
        after := c.Query("after")
        if after == "" {
            posts := store.PostsAll(perPage)
            feedsMap := store.FeedsAllMap()
            c.HTML(200, "posts.tmpl", gin.H{"posts": posts, "feeds": feedsMap, "base": baseURL})
        } else {
            id := UnhashID(after)
            p := store.PostsID(id)
            if p == nil {
                c.String(200, "Nothing found")
                return
            }
            posts := store.PostsAllAfter(perPage, p.Date)
            feedsMap := store.FeedsAllMap()
            c.HTML(200, "posts.tmpl", gin.H{"posts": posts, "feeds": feedsMap, "base": baseURL})
        }
    })
    r.GET(baseURL + "/f/:feeds", func(c *gin.Context) {
        after := c.Query("after")
        feedlist := strings.Split(c.Param("feeds"), "+")
        if after == "" {
            posts := store.PostsByFeeds(perPage, feedlist)
            log.Printf("found %d posts", len(posts))
            feedsMap := store.FeedsAllMap()
            c.HTML(200, "posts.tmpl", gin.H{"posts": posts, "feeds": feedsMap, "base": baseURL})
        } else {
            id := UnhashID(after)
            p := store.PostsID(id)
            if p == nil {
                c.String(200, "Nothing found")
                return
            }
            posts := store.PostsByFeedsAfter(perPage, feedlist, p.Date)
            feedsMap := store.FeedsAllMap()
            c.HTML(200, "posts.tmpl", gin.H{"posts": posts, "feeds": feedsMap, "base": baseURL})
        }
    })

    /*   /l/ - FEED LIST */

    r.GET(baseURL + "/l/", func(c *gin.Context) {
        feeds := store.FeedsAll()
        c.HTML(200, "feeds.tmpl", gin.H{"feeds": feeds, "base": baseURL})
    })


    /*   /a/*- ARTICLES */

    r.GET(baseURL + "/a/:articleid", func(c *gin.Context) {
        postID := UnhashID(c.Param("articleid"))
        if err != nil { c.String(200, err.Error()); return }
        post := store.PostsID(postID)
        if post == nil { c.String(200, fmt.Sprintf("invalid article: %d", postID)); return }
        r, err := store.ReadabilityGetOne(post.ID)
        if err != nil { c.String(200, err.Error()); return }
        c.HTML(200, "article.tmpl", gin.H{"title": post.Title, "content": template.HTML(r.Content), "base": baseURL})
    });

    /*   /c/*- CONTROL CENTER */

    r.GET(baseURL + "/c/", func(c *gin.Context) {
        stats := make(map[string]interface{})
        stats["num posts"] = len(store.PostsAll(-1))
        c.HTML(200, "control.tmpl", gin.H{"stats": stats})
    });

    r.POST(baseURL + "/c/", func(c *gin.Context) {
        passwdHex := c.PostForm("passwd")
        key := c.PostForm("cmd")
        val := c.PostForm("arg")
        passwd, err := hex.DecodeString(passwdHex)
        if err != nil {
            c.String(200, "Invalid password")
            return
        }
        thisHash := sha1.Sum(passwd)
        if bytes.Compare(thisHash[:], hash) != 0 {
            c.String(200, "Invalid password")
            return
        }
        c.String(200, key + " = " + val)
    })

    /*   /x/*- APIs */

    r.GET(baseURL + "/x/r/:articleid", func(c *gin.Context) {
        postID := UnhashID(c.Param("articleid"))
        if err != nil { c.String(200, err.Error()); return }
        post := store.PostsID(postID)
        if post == nil { c.String(200, fmt.Sprintf("invalid article: %d", postID)); return }
        r, err := store.ReadabilityGetOne(post.ID)
        if err != nil { c.String(200, err.Error()); return }
        js, err := json.Marshal(r)
        if err != nil { c.String(200, err.Error()); return }
        c.String(200, string(js))
    });

    /*

    r.GET("/suggest", func(c *gin.Context) {
        c.String(http.StatusOK, "Not Implemented yet");
    })
    */

    r.Run(addr)

    return nil
}

func cmdCmd() error {
    flag.Parse()
    if flag.NArg() < 2 {
        return help()
    }
    return errors.New("Not yet implemented")
}

func cmdHash() error {
    oldState, err := terminal.MakeRaw(0)
    if err != nil {
        return err
    }
    defer terminal.Restore(0, oldState)
    term := terminal.NewTerminal(os.Stdin, "")
    pw, err := term.ReadPassword("Enter password: ")
    if err != nil { return err }
    hash := sha1.Sum([]byte(pw))
    log.Printf("password Hash: %s", hex.EncodeToString(hash[:]))
    return nil
}

func cmdDump() error {
    store, err := NewStore(dbFile)
    if err != nil {
        return err
    }
    defer store.Close()

    store.Dump()
    return nil
}

type bla struct { id int64; i int64 }
type int64Slice []*bla
func (s int64Slice) Len() int { return len(s) }
func (s int64Slice) Less(i, j int) bool { return s[i].id < s[j].id }
func (s int64Slice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func cmdTest() error {
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
    feed.URL = "https://en.wikinews.org/w/index.php?title=Special:NewsFeed&feed=atom&categories=Published&notcategories=No%20publish%7CArchived%7CAutoArchived%7Cdisputed&namespace=0&count=30&hourcount=124&ordermethod=categoryadd&stablepages=only"
    feed.Handle = "wik"
    if err = store.FeedsSet(&feed); err != nil { return err }

    feed.ID = MakeID()
    feed.URL = "http://rss.csmonitor.com/feeds/csm"
    feed.Handle = "csm"
    if err = store.FeedsSet(&feed); err != nil { return err }

    feed.ID = MakeID()
    feed.URL = "http://www.aljazeera.com/xml/rss/all.xml"
    feed.Handle = "alj"
    if err = store.FeedsSet(&feed); err != nil { return err }

    feed.ID = MakeID()
    feed.URL = "http://www.economist.com/sections/culture/rss.xml"
    feed.Handle = "ecoc"
    if err = store.FeedsSet(&feed); err != nil { return err }

    feed.ID = MakeID()
    feed.URL = "http://www.economist.com/sections/international/rss.xml"
    feed.Handle = "ecoi"
    if err = store.FeedsSet(&feed); err != nil { return err }

    return nil
}

func help() error {
    log.Printf("Usage:")
    log.Printf("    go-news command")
    log.Printf("")
    log.Printf("Commands are:")
    log.Printf("    run         run news web app normally")
    log.Printf("    cmd <key>=<value>")
    log.Printf("                send command to running web app")
    log.Printf("    hash        generate hash for database")
    log.Printf("    init        (re-)initialize database")
    log.Printf("    initDbg     (re-)initialize database (fill with debug values)")
    log.Printf("    test        arbitray test code")
    log.Printf("    testFeeds   run rss daemon for a while")
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
    case cmdStr == "cmd":
        cmdFunc = cmdCmd
    case cmdStr == "hash":
        cmdFunc = cmdHash
    case cmdStr == "init":
        cmdFunc = cmdInit
    case cmdStr == "initDbg":
        cmdFunc = cmdInitDebug
    case cmdStr == "test":
        cmdFunc = cmdTest
    case cmdStr == "dump":
        cmdFunc = cmdDump
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
