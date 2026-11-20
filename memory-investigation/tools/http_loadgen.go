package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "usage: %s <url> <concurrency> <duration_sec>\n", os.Args[0])
		os.Exit(1)
	}
	url := os.Args[1]
	conc, _ := strconv.Atoi(os.Args[2])
	durSec, _ := strconv.Atoi(os.Args[3])
	tr := &http.Transport{MaxIdleConnsPerHost: conc, MaxConnsPerHost: conc}
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	deadline := time.Now().Add(time.Duration(durSec) * time.Second)
	var okCount, errCount atomic.Int64
	var wg sync.WaitGroup
	for range conc {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				resp, err := client.Get(url)
				if err != nil {
					errCount.Add(1)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == 200 {
					okCount.Add(1)
				} else {
					errCount.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	fmt.Printf("loadgen %s: ok=%d err=%d\n", url, okCount.Load(), errCount.Load())
}
