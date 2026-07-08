package logpartitions

import (
	"fmt"
	"testing"
	"time"
)

type Partition struct {
	TableName string
	Timestamp time.Time
}

func For(tableName string, timestamp time.Time) Partition {
	return Partition{TableName: tableName, Timestamp: timestamp.UTC()}
}

func Day(timestamp time.Time) time.Time {
	utc := timestamp.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func Ensure(tb testing.TB, partitions []Partition, ensure func(tableName string, day time.Time) error) {
	tb.Helper()
	seen := make(map[string]struct{}, len(partitions))
	for _, partition := range partitions {
		day := Day(partition.Timestamp)
		key := partition.TableName + ":" + day.Format("20060102")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if err := ensure(partition.TableName, day); err != nil {
			tb.Fatalf("ensure %s partition for %s: %v", partition.TableName, day.Format("2006-01-02"), err)
		}
	}
}

func QuoteTimestamp(timestamp time.Time) string {
	return "'" + timestamp.UTC().Format("2006-01-02 15:04:05-07:00") + "'"
}

func PartitionName(tableName string, day time.Time) string {
	return fmt.Sprintf("%s_p%s", tableName, Day(day).Format("20060102"))
}
