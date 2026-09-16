package traffic

import (
	"crypto/cipher"
	"database/sql"
	"errors"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"time"
)

type DB struct {
	sql *sql.DB
	key cipher.AEAD
}
type Sample struct {
	Router, Kind, Device, Epoch, Name, IP string
	At, Down, Up                          int64
	DownRate, UpRate                      int64
}
type Device struct {
	MAC              string `json:"mac"`
	Name             string `json:"name"`
	IP               string `json:"ip"`
	Download         int64  `json:"download"`
	Upload           int64  `json:"upload"`
	CurrentDownload  int64  `json:"currentDownload"`
	CurrentUpload    int64  `json:"currentUpload"`
	BaselineDownload int64  `json:"baselineDownload"`
	BaselineUpload   int64  `json:"baselineUpload"`
	LastAt           int64  `json:"lastAt"`
	FirstAt          int64  `json:"firstAt"`
	Infrastructure   bool   `json:"infrastructure"`
}
type Bucket struct {
	At       int64 `json:"at"`
	Download int64 `json:"download"`
	Upload   int64 `json:"upload"`
}
type Report struct {
	From          int64    `json:"from"`
	To            int64    `json:"to"`
	Devices       []Device `json:"devices"`
	Clash         []Bucket `json:"clash"`
	Download      int64    `json:"download"`
	Upload        int64    `json:"upload"`
	Gaps          int64    `json:"gaps"`
	FirstAt       int64    `json:"firstAt"`
	RetentionDays int      `json:"retentionDays"`
}

func OpenDB(dir string) (*DB, error) {
	key, e := openKey(dir)
	if e != nil {
		return nil, e
	}
	p := filepath.Join(dir, "traffic.db")
	db, e := sql.Open("sqlite", p)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	d := &DB{db, key}
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS config(router TEXT PRIMARY KEY, sealed BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS state(router TEXT,kind TEXT,device TEXT,epoch TEXT,at INTEGER,down INTEGER,up INTEGER,name TEXT,ip TEXT,first_at INTEGER,baseline_down INTEGER,baseline_up INTEGER,PRIMARY KEY(router,kind,device));
 CREATE TABLE IF NOT EXISTS bucket(router TEXT,kind TEXT,device TEXT,at INTEGER,down INTEGER,up INTEGER,PRIMARY KEY(router,kind,device,at));
 CREATE INDEX IF NOT EXISTS bucket_time ON bucket(router,at);
 CREATE TABLE IF NOT EXISTS gap(router TEXT,kind TEXT,device TEXT,from_at INTEGER,to_at INTEGER,reason TEXT);
 CREATE INDEX IF NOT EXISTS gap_time ON gap(router,to_at);`)
	if e != nil {
		db.Close()
		return nil, e
	}
	if e = os.Chmod(p, 0600); e != nil {
		db.Close()
		return nil, e
	}
	return d, nil
}
func (d *DB) Close() error { return d.sql.Close() }
func (d *DB) Configs() ([]Config, error) {
	rows, e := d.sql.Query("SELECT router,sealed FROM config")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Config{}
	for rows.Next() {
		var id string
		var b []byte
		if e = rows.Scan(&id, &b); e != nil {
			return nil, e
		}
		c, e := unseal(d.key, id, b)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (d *DB) SaveConfig(c Config) error {
	b, e := seal(d.key, c)
	if e != nil {
		return e
	}
	_, e = d.sql.Exec("INSERT INTO config VALUES(?,?) ON CONFLICT(router) DO UPDATE SET sealed=excluded.sealed", c.RouterID, b)
	return e
}

// Record never attributes pre-existing counters to today. A new stream/session,
// decrease, or long sampling gap starts a new baseline and records a gap, not an
// invented byte count. Known uninterrupted deltas are split at UTC minute edges.
func (d *DB) Record(s Sample, maxGap time.Duration) error {
	if s.Down < 0 || s.Up < 0 || s.At <= 0 || s.Epoch == "" {
		return errors.New("invalid sample")
	}
	tx, e := d.sql.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var epoch string
	var at, down, up int64
	e = tx.QueryRow("SELECT epoch,at,down,up FROM state WHERE router=? AND kind=? AND device=?", s.Router, s.Kind, s.Device).Scan(&epoch, &at, &down, &up)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if e == sql.ErrNoRows {
		_, e = tx.Exec("INSERT INTO state VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", s.Router, s.Kind, s.Device, s.Epoch, s.At, s.Down, s.Up, s.Name, s.IP, s.At, s.Down, s.Up)
	} else {
		if s.At <= at {
			return nil
		} // duplicate delivery and backwards wall clock are not traffic
		reason := ""
		switch {
		case epoch != s.Epoch:
			reason = "session-change"
		case s.Down < down || s.Up < up:
			reason = "counter-reset"
		case s.At-at > maxGap.Milliseconds():
			reason = "sampling-gap"
		}
		if reason != "" {
			_, e = tx.Exec("INSERT INTO gap VALUES(?,?,?,?,?,?)", s.Router, s.Kind, s.Device, at, s.At, reason)
		} else {
			elapsed := s.At - at
			dd, du := s.Down-down, s.Up-up
			// Cumulative fractions ensure byte-exact conservation across bucket edges.
			var usedD, usedU int64
			for start := at; start < s.At && (dd != 0 || du != 0); {
				minute := start / 60000 * 60000
				end := minute + 60000
				if end > s.At {
					end = s.At
				}
				// Counts from a few-second interval; quotient/remainder avoids int64 multiplication overflow.
				frac := func(n int64) int64 { return n/elapsed*(end-at) + (n%elapsed)*(end-at)/elapsed }
				nd, nu := frac(dd), frac(du)
				_, e = tx.Exec(`INSERT INTO bucket VALUES(?,?,?,?,?,?) ON CONFLICT(router,kind,device,at) DO UPDATE SET down=down+excluded.down,up=up+excluded.up`, s.Router, s.Kind, s.Device, minute, nd-usedD, nu-usedU)
				if e != nil {
					return e
				}
				usedD, usedU = nd, nu
				start = end
			}
		}
		if e == nil {
			_, e = tx.Exec("UPDATE state SET epoch=?,at=?,down=?,up=?,name=?,ip=? WHERE router=? AND kind=? AND device=?", s.Epoch, s.At, s.Down, s.Up, s.Name, s.IP, s.Router, s.Kind, s.Device)
		}
	}
	if e != nil {
		return e
	}
	return tx.Commit()
}

func (d *DB) Report(router string, from, to int64) (Report, error) {
	out := Report{From: from, To: to, Devices: []Device{}, Clash: []Bucket{}, RetentionDays: 365}
	rows, e := d.sql.Query(`SELECT s.device,s.name,s.ip,s.down,s.up,s.first_at,s.at,s.baseline_down,s.baseline_up,COALESCE(b.down,0),COALESCE(b.up,0) FROM state s LEFT JOIN (SELECT device,SUM(down) down,SUM(up) up FROM bucket WHERE router=? AND kind='device' AND at>=? AND at<? GROUP BY device) b ON b.device=s.device WHERE s.router=? AND s.kind='device'`, router, from, to, router)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var x Device
		e = rows.Scan(&x.MAC, &x.Name, &x.IP, &x.CurrentDownload, &x.CurrentUpload, &x.FirstAt, &x.LastAt, &x.BaselineDownload, &x.BaselineUpload, &x.Download, &x.Upload)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Devices = append(out.Devices, x)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	// At most one hourly point per hour; a year never returns 525,600 points.
	rows, e = d.sql.Query("SELECT (at/3600000)*3600000,SUM(down),SUM(up) FROM bucket WHERE router=? AND kind='clash' AND at>=? AND at<? GROUP BY at/3600000 ORDER BY at/3600000", router, from, to)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var b Bucket
		if e = rows.Scan(&b.At, &b.Download, &b.Upload); e != nil {
			rows.Close()
			return out, e
		}
		out.Clash = append(out.Clash, b)
		out.Download += b.Download
		out.Upload += b.Upload
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	e = d.sql.QueryRow("SELECT COUNT(*) FROM gap WHERE router=? AND to_at>? AND from_at<?", router, from, to).Scan(&out.Gaps)
	if e != nil {
		return out, e
	}
	e = d.sql.QueryRow("SELECT COALESCE(MIN(first_at),0) FROM state WHERE router=?", router).Scan(&out.FirstAt)
	return out, e
}
func (d *DB) Prune(now time.Time) error {
	cutoff := now.Add(-365 * 24 * time.Hour).UnixMilli()
	_, e := d.sql.Exec("DELETE FROM bucket WHERE at<?; DELETE FROM gap WHERE to_at<?", cutoff, cutoff)
	return e
}
