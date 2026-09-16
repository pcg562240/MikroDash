package traffic

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	d, e := OpenDB(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	return d
}
func sample(at, down, up int64) Sample {
	return Sample{Router: "r", Kind: "clash", Device: "core", Epoch: "one", At: at, Down: down, Up: up}
}
func total(t *testing.T, d *DB) (int64, int64, int64) {
	t.Helper()
	r, e := d.Report("r", 0, 9999999999999)
	if e != nil {
		t.Fatal(e)
	}
	return r.Download, r.Upload, r.Gaps
}
func record(t *testing.T, d *DB, s Sample) {
	t.Helper()
	if e := d.Record(s, 45*time.Second); e != nil {
		t.Fatal(e)
	}
}
func TestFirstSampleIsBaseline(t *testing.T) {
	d := testDB(t)
	record(t, d, sample(1000, 4000000000, 2000000000))
	a, b, g := total(t, d)
	if a != 0 || b != 0 || g != 0 {
		t.Fatal(a, b, g)
	}
}

func TestIdleDevicesDoNotFillMinuteBuckets(t *testing.T) {
	d := testDB(t)
	record(t, d, sample(1000, 10, 20))
	record(t, d, sample(2000, 10, 20))
	var n int
	if e := d.sql.QueryRow("SELECT COUNT(*) FROM bucket").Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
}
func TestDeltasAndMinuteBoundaries(t *testing.T) {
	d := testDB(t)
	record(t, d, sample(59000, 100, 200))
	record(t, d, sample(62000, 109, 206))
	a, b, _ := total(t, d)
	if a != 9 || b != 6 {
		t.Fatal(a, b)
	}
	var down int
	if e := d.sql.QueryRow("SELECT down FROM bucket WHERE at=0").Scan(&down); e != nil || down != 3 {
		t.Fatal(down, e)
	}
}
func TestCountersResetWithoutSpikes(t *testing.T) {
	d := testDB(t)
	record(t, d, sample(1000, 10000, 20000))
	record(t, d, sample(2000, 50, 60))
	record(t, d, sample(3000, 55, 67))
	a, b, g := total(t, d)
	if a != 5 || b != 7 || g != 1 {
		t.Fatal(a, b, g)
	}
}
func TestNewStreamDoesNotAssumeSameCore(t *testing.T) {
	d := testDB(t)
	record(t, d, sample(1000, 10, 20))
	s := sample(2000, 999999, 999999)
	s.Epoch = "new"
	record(t, d, s)
	a, b, g := total(t, d)
	if a != 0 || b != 0 || g != 1 {
		t.Fatal(a, b, g)
	}
}
func TestLongGapDoesNotInventDailyAllocation(t *testing.T) {
	d := testDB(t)
	record(t, d, sample(1000, 10, 20))
	record(t, d, sample(100000, 999, 999))
	a, b, g := total(t, d)
	if a != 0 || b != 0 || g != 1 {
		t.Fatal(a, b, g)
	}
}
func TestDuplicateAndBackwardsSamplesIgnored(t *testing.T) {
	d := testDB(t)
	record(t, d, sample(1000, 10, 20))
	record(t, d, sample(1000, 999, 999))
	record(t, d, sample(999, 999, 999))
	record(t, d, sample(2000, 20, 40))
	a, b, g := total(t, d)
	if a != 10 || b != 20 || g != 0 {
		t.Fatal(a, b, g)
	}
}
func TestDeviceQueriesIsolateRouters(t *testing.T) {
	d := testDB(t)
	s := sample(1000, 10, 20)
	s.Kind = "device"
	s.Device = "02:00:00:00:00:01"
	record(t, d, s)
	s.At = 2000
	s.Down = 123
	s.Up = 45
	record(t, d, s)
	r, e := d.Report("r", 0, 99999999)
	if e != nil || len(r.Devices) != 1 || r.Devices[0].Download != 113 || r.Download != 0 {
		t.Fatal(r, e)
	}
	other, e := d.Report("other", 0, 999999)
	if e != nil || len(other.Devices) != 0 {
		t.Fatal(other, e)
	}
}
func TestPersistenceAndEncryptedConfig(t *testing.T) {
	dir := t.TempDir()
	d, e := OpenDB(dir)
	if e != nil {
		t.Fatal(e)
	}
	c := Config{RouterID: "r", ClashSecret: "private-api-token", ROS: ROSConfig{Password: "private-ros-password"}}
	if e = d.SaveConfig(c); e != nil {
		t.Fatal(e)
	}
	record(t, d, sample(1000, 1, 2))
	record(t, d, sample(2000, 11, 22))
	d.Close()
	raw, _ := os.ReadFile(filepath.Join(dir, "traffic.db"))
	if strings.Contains(string(raw), c.ClashSecret) || strings.Contains(string(raw), c.ROS.Password) {
		t.Fatal("plaintext credentials")
	}
	d, e = OpenDB(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	cfgs, e := d.Configs()
	if e != nil || cfgs[0].ClashSecret != c.ClashSecret {
		t.Fatal(e)
	}
	a, b, _ := total(t, d)
	if a != 10 || b != 20 {
		t.Fatal(a, b)
	}
	pub := c.Public()
	if strings.Contains(strings.TrimSpace(toJSON(pub)), c.ClashSecret) {
		t.Fatal("public secret")
	}
}
func TestLostKeyFailsClosed(t *testing.T) {
	dir := t.TempDir()
	d, e := OpenDB(dir)
	if e != nil {
		t.Fatal(e)
	}
	d.Close()
	os.Remove(filepath.Join(dir, ".key"))
	if _, e = OpenDB(dir); e == nil {
		t.Fatal("must not regenerate key over existing database")
	}
}
func TestPeriodShanghaiBoundary(t *testing.T) {
	now := time.Date(2026, 9, 16, 17, 0, 0, 0, time.UTC)
	from, to, e := period(url.Values{"period": {"today"}}, now)
	if e != nil {
		t.Fatal(e)
	}
	if time.UnixMilli(from).UTC().Hour() != 16 || to-from != 86400000 {
		t.Fatal(from, to)
	}
	if time.UnixMilli(from).UTC().Day() != 16 {
		t.Fatal(from)
	}
}
func TestPeriodRejectsInvalidAndFutureRanges(t *testing.T) {
	for _, q := range []url.Values{{"period": {"bogus"}}, {"period": {"custom"}, "from": {"2020-01-01"}, "to": {"2030-01-01"}}, {"period": {"custom"}, "from": {"2026-09-17"}, "to": {"2026-09-16"}}} {
		if _, _, e := period(q, time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)); e == nil {
			t.Fatal(q)
		}
	}
}

func TestRetentionUses365DaysAcrossLeapYear(t *testing.T) {
	d := testDB(t)
	now := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	cutoff := now.Add(-365 * 24 * time.Hour).UnixMilli()
	for _, at := range []int64{cutoff - 60000, cutoff} {
		if _, e := d.sql.Exec("INSERT INTO bucket VALUES('r','clash','core',?,1,2)", at); e != nil {
			t.Fatal(e)
		}
	}
	if e := d.Prune(now); e != nil {
		t.Fatal(e)
	}
	a, b, _ := total(t, d)
	if a != 1 || b != 2 {
		t.Fatal("retention must not keep an extra leap day", a, b)
	}
}
