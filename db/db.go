package db;

import (
	"time"
	"strings"
	"fmt"
	"errors"
	"database/sql"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	_ "github.com/lib/pq"
	_ "github.com/ziutek/mymysql"
)

type Feed struct {
	ID     int64  `db:"id"`
	Handle string `db:"handle"`
	Title  string `db:"title"`
	Link   string `db:"link"`
	URL    string `db:"url"`
}

type Post struct {
	ID      int64      `db:"id"`
	Title   string     `db:"title"`
	GUID    string     `db:"guid"`
	Link    string     `db:"link"`
	Feed    int64      `db:"feed"`
	Date    time.Time  `db:"time"`
	Content string     `db:"content"`
}

type FeedReq struct {
	ID   int64
	URL  string
	N    int
	Date time.Time
}

var (
	db *sqlx.DB = nil
	connString string = ""
)

var schema = `
CREATE TABLE IF NOT EXISTS settings (
	name TEXT PRIMARY KEY UNIQUE,
	value TEXT 
);
CREATE TABLE IF NOT EXISTS feeds (
	id INTEGER PRIMARY KEY NOT NULL,
	handle TEXT UNIQUE,
	title TEXT,
	link TEXT,
	url TEXT UNIQUE
);
CREATE TABLE IF NOT EXISTS posts (
	id INTEGER PRIMARY KEY NOT NULL,
	title TEXT,
	guid TEXT UNIQUE,
	link TEXT,
	feed INTEGER,
	time DATETIME,
	content TEXT,
	FOREIGN KEY(feed) REFERENCES feeds(id)
);
CREATE TABLE IF NOT EXISTS requests (
	id INTEGER PRIMARY KEY NOT NULL,
	url TEXT,
	num INTEGER
);
`
///////////////////////////////////////////////////////////
// general functionality

func Connect(url string) error {
	if url == "" {
		return errors.New("invalid connection string")
	}
	if db != nil && url != connString {
		return errors.New("cannot reconnect without exiting")
	}
	if db != nil {
		return nil
	}
	parts := strings.SplitN(url, "://", 2)
	if len(parts) < 2 {
		return errors.New("conneciton string must be <driver>://<source>")
	}

	var err error
	if db, err = sqlx.Connect(parts[0], parts[1]); err != nil {
		return err
	}

	if _, err = db.Exec(schema); err != nil {
		return err
	}

	connString = url
	return nil
}

func Disconnect() {
	if db == nil {
		return
	}
	if err := db.Close(); err != nil {
		panic(err)
	}
	db = nil
	connString = ""
}

func mustDb() *sqlx.DB {
	if db == nil {
		panic("trying to use uninitialized database")
	}
	return db
}

///////////////////////////////////////////////////////////
// feed management

// ID     int64  `db:"id"`
// Handle string `db:"handle"`
// Title  string `db:"title"`
// Link   string `db:"link"`
// URL    string `db:"url"`

func FeedAdd(feed *Feed) (int64, error) {
	query := "INSERT INTO feeds(handle, url) VALUES (:handle, :url)"
	if res, err := mustDb().NamedExec(query, feed); err != nil {
		return -1, err
	} else if id, err := res.LastInsertId(); err != nil {
		return -1, err
	} else {
		feed.ID = id
		return id, nil
	}
}

func FeedUpdate(feed *Feed) error {
	query := "UPDATE feeds SET handle = :handle, URL = :url, title = :title, link = :link WHERE id = :id"
	res, _ := mustDb().NamedExec(query, feed)
	if nrows, err := res.RowsAffected(); err != nil {
		return err
	} else if nrows == 0 {
		return errors.New("invalid feed")
	} else {
		return nil
	}
}

func FeedRemoveByHandleOrURL(handleOrURL string) error {
	query := "DELETE FROM feeds WHERE handle = $1 or url = $1";
	res, _ := mustDb().Exec(query, handleOrURL)
	if nrows, err := res.RowsAffected(); err != nil {
		return err
	} else if nrows == 0 {
		return errors.New("no feed with that handle or url")
	} else {
		return nil
	}
}

func FeedAll() ([]*Feed, error) {
	query := `SELECT * FROM feeds;`
	feeds := []*struct{
		ID int64
		Handle string
		Title  sql.NullString
		Link   sql.NullString
		URL    string
	}{}
	if err := mustDb().Select(&feeds, query); err != nil {
		return nil, err
	}
	feedsReal := []*Feed{}
	for _, f := range(feeds) {
		feedsReal = append(feedsReal, &Feed{f.ID, f.Handle, f.Title.String, f.Link.String, f.URL})
	}
	return feedsReal, nil
}


///////////////////////////////////////////////////////////
// post management

// ID      int64      `db:"id"`
// Title   string     `db:"title"`
// GUID    string     `db:"guid"`
// Link    string     `db:"link"`
// Feed    int64      `db:"feed"`
// Date    time.Time  `db:"time"`
// Content string     `db:"content"`

func PostAdd(post *Post) (int64, error) {
	query := `INSERT INTO posts(title, guid, link, feed, time)
		VALUES (:title, :guid, :link, :feed, :time)`
	if res, err := mustDb().NamedExec(query, post); err != nil {
		return -1, err
	} else if id, err := res.LastInsertId(); err != nil {
		return -1, err
	} else {
		post.ID = id
		return id, nil
	}
}

func PostAddBatch(posts []*Post) error {
	nerr := 0
	for _, post := range(posts) {
		_, err := PostAdd(post)
		if err != nil {
			nerr += 1
		}
	}
	if nerr > 0 {
		return errors.New(fmt.Sprintf("failed to add %d posts", nerr))
	} else {
		return nil
	}
}

func PostNAfter(n int, after time.Time) ([]*Post, error) {
	query := `SELECT id,title,guid,link,feed,date FROM posts WHERE time < $2 LIMIT $1 ORDER BY time DESC;`
	posts := []*struct{
		ID     int64
		Title  sql.NullString
		GUID   sql.NullString
		Link   sql.NullString
		Feed   int64
		Date   time.Time
	}{}
	if err := mustDb().Select(&posts, query, n, after); err != nil {
		return nil, err
	}
	postsReal := []*Post{}
	for _, p := range(posts) {
		postsReal = append(postsReal, &Post{
			p.ID,
			p.Title.String,
			p.GUID.String,
			p.Link.String,
			p.Feed,
			p.Date,
			"",
		})
	}
	return postsReal, nil
}

///////////////////////////////////////////////////////////
// content management

func PostFetchContent(post *Post) error {
	query := `SELECT content FROM posts WHERE id = $1`
	postContent := []struct{
		Content sql.NullString
	}{}
	if _, err := mustDb().Select(&postContent, query, post.ID); err != nil {
		return err
	}
	// post content already fetched, we're good, just update and return
	if postContent.Valid {
		post.Content = postContent.String
		return nil
	}
	// post content not yet fetched, so we have to do that and update the database
}
