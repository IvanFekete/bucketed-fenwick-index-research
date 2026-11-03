// Command lichess streams a Lichess Open Database dump (.pgn.zst) from stdin
// and writes one CSV row per rated game with the fields the paper needs:
//
//	ts_unix,speed,base_s,inc_s,white_elo,black_elo
//
// Only PGN header lines are parsed; move text is skipped. The dump is never
// written to disk, so a multi-GB month can be processed with
//
//	curl -s URL | go run ./tools/lichess -max-games 20000000 | gzip > out.csv.gz
//
// speed is derived from the Event header ("Rated Blitz game" -> blitz).
// Games with a missing or provisional ("?") rating are skipped and counted.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

type game struct {
	event, date, clock, tc, welo, belo string
}

func (g *game) reset() { *g = game{} }

func headerValue(line string) (key, val string, ok bool) {
	// [Key "Value"]
	if len(line) < 5 || line[0] != '[' || line[len(line)-1] != ']' {
		return "", "", false
	}
	sp := strings.IndexByte(line, ' ')
	if sp < 0 {
		return "", "", false
	}
	key = line[1:sp]
	rest := line[sp+1 : len(line)-1]
	if len(rest) < 2 || rest[0] != '"' || rest[len(rest)-1] != '"' {
		return "", "", false
	}
	return key, rest[1 : len(rest)-1], true
}

func speedOf(event string) string {
	e := strings.ToLower(event)
	for _, s := range []string{"ultrabullet", "bullet", "blitz", "rapid", "classical", "correspondence"} {
		if strings.Contains(e, s) {
			return s
		}
	}
	return "other"
}

func main() {
	maxGames := flag.Int64("max-games", 0, "stop after this many emitted rows (0 = no limit)")
	untilStr := flag.String("until", "", "stop at the first game whose UTC start is >= this RFC3339 time")
	flag.Parse()

	var until time.Time
	if *untilStr != "" {
		t, err := time.Parse(time.RFC3339, *untilStr)
		if err != nil {
			fmt.Fprintln(os.Stderr, "bad -until:", err)
			os.Exit(2)
		}
		until = t
	}

	dec, err := zstd.NewReader(bufio.NewReaderSize(os.Stdin, 1<<20), zstd.WithDecoderConcurrency(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "zstd:", err)
		os.Exit(1)
	}
	defer dec.Close()

	in := bufio.NewReaderSize(dec, 4<<20)
	out := bufio.NewWriterSize(os.Stdout, 4<<20)
	defer out.Flush()
	fmt.Fprintln(out, "ts_unix,speed,base_s,inc_s,white_elo,black_elo")

	var g game
	var emitted, skipped, seen int64
	inHeader := false
	start := time.Now()

	flush := func() bool {
		seen++
		defer g.reset()
		if g.welo == "" || g.belo == "" || g.welo == "?" || g.belo == "?" {
			skipped++
			return true
		}
		t, err := time.Parse("2006.01.02 15:04:05", g.date+" "+g.clock)
		if err != nil {
			skipped++
			return true
		}
		if !until.IsZero() && !t.Before(until) {
			return false
		}
		base, inc := "", ""
		if p := strings.IndexByte(g.tc, '+'); p > 0 {
			base, inc = g.tc[:p], g.tc[p+1:]
		}
		fmt.Fprintf(out, "%d,%s,%s,%s,%s,%s\n", t.Unix(), speedOf(g.event), base, inc, g.welo, g.belo)
		emitted++
		if emitted%5_000_000 == 0 {
			fmt.Fprintf(os.Stderr, "%d rows, last %s, %.0fs\n", emitted, t.Format(time.RFC3339), time.Since(start).Seconds())
		}
		return *maxGames == 0 || emitted < *maxGames
	}

	for {
		line, err := in.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if len(line) > 0 && line[0] == '[' {
			if !inHeader {
				inHeader = true
			}
			if k, v, ok := headerValue(line); ok {
				switch k {
				case "Event":
					g.event = v
				case "UTCDate":
					g.date = v
				case "UTCTime":
					g.clock = v
				case "TimeControl":
					g.tc = v
				case "WhiteElo":
					g.welo = v
				case "BlackElo":
					g.belo = v
				}
			}
		} else if inHeader && len(line) > 0 {
			// First move-text line ends the header block.
			inHeader = false
			if !flush() {
				break
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			// A truncated stream (e.g. curl stopped by -until) is expected.
			fmt.Fprintln(os.Stderr, "read:", err)
			break
		}
	}
	fmt.Fprintf(os.Stderr, "done: emitted=%d skipped=%d seen=%d in %s\n", emitted, skipped, seen, time.Since(start).Round(time.Second))
}
