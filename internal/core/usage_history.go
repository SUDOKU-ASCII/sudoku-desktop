package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type usageHistoryFile struct {
	Version int        `json:"version"`
	Days    []UsageDay `json:"days"`
}

func loadUsageHistory(path string) []UsageDay {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f usageHistoryFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil
	}
	days := append([]UsageDay(nil), f.Days...)
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	return days
}

func saveUsageHistory(path string, days []UsageDay) error {
	f := usageHistoryFile{
		Version: 1,
		Days:    append([]UsageDay(nil), days...),
	}
	sort.Slice(f.Days, func(i, j int) bool { return f.Days[i].Date < f.Days[j].Date })
	buf, err := json.MarshalIndent(&f, "", "  ")
	if err != nil {
		return err
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644)
}

func usageDayKey(t time.Time) string {
	return t.Format("2006-01-02")
}

func addUsageToDay(days []UsageDay, key string, deltaTx, deltaRx, directTx, directRx, proxyTx, proxyRx uint64) []UsageDay {
	for i := range days {
		if days[i].Date == key {
			days[i].Tx += deltaTx
			days[i].Rx += deltaRx
			days[i].DirectTx += directTx
			days[i].DirectRx += directRx
			days[i].ProxyTx += proxyTx
			days[i].ProxyRx += proxyRx
			return days
		}
	}
	days = append(days, UsageDay{
		Date:     key,
		Tx:       deltaTx,
		Rx:       deltaRx,
		DirectTx: directTx,
		DirectRx: directRx,
		ProxyTx:  proxyTx,
		ProxyRx:  proxyRx,
	})
	return days
}

func trimUsageDays(days []UsageDay, max int) []UsageDay {
	if max <= 0 || len(days) <= max {
		return days
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	return append([]UsageDay(nil), days[len(days)-max:]...)
}

func validateUsageDay(day UsageDay) error {
	if strings.TrimSpace(day.Date) == "" {
		return fmt.Errorf("empty usage day date")
	}
	return nil
}
