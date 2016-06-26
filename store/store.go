package store;

import (
    "time"
    "encoding/hex"
    "database/sql"
    _ "github.com/mattn/go-sqlite3"
    "log"
    "errors"
    "os"
    )

type Feed struct {
    Id          uint32
    Initialized bool
    Handle      string
    Title       string
    Link        string
    Url         string
    ImageUrl    string
}

type Post struct {
    Id      uint32
    Title   string
    Guid    string
    Link    string
    Feed    uint32
    Date    time.Time
}

type Content struct {
    Id          uint32
    ArticleId   uint32
    Content     string
}

type Config struct {
    IdState     uint32
}

const path = "./data.sqlite"
var db *sql.DB
var stmts = make(map[string]*sql.Stmt)

const salt uint64 = 0xb8490f2442d1e80
func Hash(num uint64) string {
    var bytes [8]byte
    num = num ^ salt
    bytes[7] = (byte)(num >> 0)  & 0xff
    bytes[6] = (byte)(num >> 8)  & 0xff
    bytes[5] = (byte)(num >> 16) & 0xff
    bytes[4] = (byte)(num >> 24) & 0xff
    bytes[3] = (byte)(num >> 32) & 0xff
    bytes[2] = (byte)(num >> 40) & 0xff
    bytes[1] = (byte)(num >> 48) & 0xff
    bytes[0] = (byte)(num >> 56) & 0xff
    return hex.EncodeToString(bytes[:])
}

func Unhash(h string) uint64 {
    bytes, err := hex.DecodeString(h)
    if err != nil {
        return 0xffffffffffffffff
    }
    var num uint64
    num |= uint64(bytes[7]) <<  0
    num |= uint64(bytes[6]) <<  8
    num |= uint64(bytes[5]) << 16
    num |= uint64(bytes[4]) << 24
    num |= uint64(bytes[3]) << 32
    num |= uint64(bytes[2]) << 40
    num |= uint64(bytes[1]) << 48
    num |= uint64(bytes[0]) << 56
    return num ^ salt
}

func Reset() error {
    var err error
    if db != nil {
        return errors.New("Can't reset while database is opened")
    }
    log.Printf("Removing old database")
    os.Remove(path)
    db, err = sql.Open("sqlite3", path)
    if err != nil {
        return err
        //return errors.NewError("Error creating database")
    }
    sqlStmt := `
    create table Feeds (Id integer not null primary key,
                        Handle text,
                        Initialized integer,
                        Title text,
                        Link text,
                        Url text unique,
                        ImageUrl text);
    create table Posts (Id integer not null primary key,
                        Title text,
                        Guid text unique,
                        Link text,
                        Feed integer,
                        Date integer);
    create table Content (Id integer not null primary key,
                        ArticleId integer,
                        Content string);
    `
    log.Printf("Creating tables")
    _, err = db.Exec(sqlStmt)
    if err != nil {
        return err;
    }
    db.Close()
    db = nil
    log.Printf("Finished")
    return nil
}

func Dump() error {
    db, err := sql.Open("sqlite3", path)
    if err != nil { return err }
    defer func () { db.Close(); db = nil; }()

    var rows *sql.Rows

    log.Printf("Contents of table Feeds")
    rows, err = db.Query("SELECT * FROM Feeds;")
    if err != nil { return err }
    defer rows.Close()
    for rows.Next() {
        var Id uint64
        var Handle string
        var Initialized int
        var Title sql.NullString
        var Link sql.NullString
        var Url string
        var ImageUrl sql.NullString
        err = rows.Scan(&Id, &Handle, &Initialized, &Title, &Link, &Url, &ImageUrl)
        if err != nil { return err }
        log.Printf("%x, %s, %d, %s, %s, %s, %s", Id, Handle, Initialized, Title.String, Link.String, Url, ImageUrl.String)
    }

    return nil
}

func Init() error {
    var err error
    var stmt *sql.Stmt
    db, err = sql.Open("sqlite3", path)
    if err != nil { return err }

    stmt, err = db.Prepare("INSERT OR FAIL INTO Feeds(Url, Handle, Initialized) VALUES (?, ?, 0);")
    if (err != nil) { return err }
    stmts["feeds-insert"] = stmt

    stmt, err = db.Prepare("SELECT Id, Initialized, Handle, Url FROM Feeds;")
    if (err != nil) { return err }
    stmts["feeds-url"] = stmt

    stmt, err = db.Prepare("SELECT * FROM Feeds;")
    if (err != nil) { return err }
    stmts["feeds-info"] = stmt

    stmt, err = db.Prepare("UPDATE OR FAIL Feeds SET Title = ?, Link = ?, ImageUrl = ?, Initialized = 1 WHERE Id == ?");
    if (err != nil) { return err }
    stmts["feeds-update"] = stmt

    stmt, err = db.Prepare("INSERT OR IGNORE INTO Posts(Title, Guid, Link, Feed, Date) Values(?,?,?,?,?);")
    if (err != nil) { return err }
    stmts["posts-add"] = stmt

    return nil
}

func Deinit() {
    db.Close()
    db = nil
}

func FeedsAdd(f *Feed) error {
    res, err := stmts["feeds-insert"].Exec(f.Url, f.Handle);
    if err != nil { return err }
    if rows, err := res.RowsAffected(); err != nil || rows < 1 { return errors.New("Feed already exists") }
    return nil
}

func FeedsUpdate(f *Feed) error {
    res, err := stmts["feeds-update"].Exec(f.Title, f.Link, f.ImageUrl, f.Id);
    if err != nil { return err }
    if rows, err := res.RowsAffected(); err != nil || rows < 1 { return errors.New("Feed does not exist") }
    return nil
}

func FeedsUrl() ([]*Feed, error) {
    res := make([]*Feed, 0)
    rows, err := stmts["feeds-url"].Query()
    if err != nil { return nil, err }
    defer rows.Close()
    for rows.Next() {
        var Id          uint32
        var Initialized bool
        var Handle      string
        var Url         string
        err = rows.Scan(&Id, &Initialized, &Handle, &Url)
        if err != nil { return nil, err }
        res = append(res, &Feed{Id, Initialized, Handle, "", "", Url, ""})
    }
    return res, nil
}

func PostsAdd(p *Post) {
    stmts["posts-add"].Exec(p.Title, p.Guid, p.Link, p.Feed, p.Date);
}

func PostsAddBatch(posts []*Post) {
    if tx, err := db.Begin(); err == nil {
        defer tx.Commit()
        stmt, err := tx.Prepare("INSERT OR IGNORE INTO Posts(Title, Guid, Link, Feed, Date) Values(?,?,?,?,?);")
        if err != nil {
            log.Fatal("[PostsAddBatch] %s", err.Error())
            return
        }
        for _, p := range(posts) {
            stmt.Exec(p.Title, p.Guid, p.Link, p.Feed, p.Date);
        }
    } else {
        log.Fatal("[PostsAddBatch] %s", err.Error())
    }
}

func PostsRecent() ([]*Post, error) {
    return nil, nil
}

func PostsRecentFeeds() ([]*Post, error) {
    return nil, nil
}
