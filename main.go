package main

import (
	"flag"
	"fmt"
	"math"
	"net"
	"os"

	"time"

	"github.com/hirose31/ringbuffer"
	termbox "github.com/nsf/termbox-go"
	"github.com/tatsushid/go-fastping"
)

type response struct {
	addr *net.IPAddr
	rtt  time.Duration
}

func keyEventLoop(kch chan termbox.Key) {
	for {
		switch ev := termbox.PollEvent(); ev.Type {
		case termbox.EventKey:
			kch <- ev.Key
		case termbox.EventResize:
			layout.termW, layout.termH = termbox.Size()
			drawHeader()
		default:
		}
	}
}

func drawStr(x, y int, str string) {
	drawColorfulStr(x, y, str, termbox.ColorDefault, termbox.ColorDefault)
}

func drawColorfulStr(x, y int, str string, fg, bg termbox.Attribute) {
	runes := []rune(str)
	for i := 0; i < len(runes); i++ {
		if runes[i] == 'x' { // Change "x" to a red cross (✗)
			termbox.SetCell(x+i, y, '✗', termbox.ColorRed, bg)
		} else if runes[i] == '✓' { // Change "✓" to a green tick (✓)
			termbox.SetCell(x+i, y, '✓', termbox.ColorGreen, bg)
		} else {
			termbox.SetCell(x+i, y, runes[i], fg, bg)
		}
	}
}

var layout struct {
	resultL        int
	resultH        int
	hostnameL      int
	termW          int
	termH          int
	failedHistoryY int
}

func drawHeader() {
	header := "PINGSWEEP: 2024 Dave Duke"
	drawStr(0, 0, header)
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s hostnames/ipaddresses seperated by spaces\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	hostnames := flag.Args()
	if len(hostnames) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	p := fastping.NewPinger()
	netProto := "ip4:icmp"
	p.MaxRTT = time.Second

	result := make(map[string]*response)
	rtt := make(map[string]*ringbuffer.RingBuffer)
	ipaddrOf := make(map[string]string)
	for _, hostname := range hostnames {
		ra, err := net.ResolveIPAddr(netProto, hostname)
		if err != nil {
			panic(err)
		}

		p.AddIPAddr(ra)

		ipaddr := ra.String()
		ipaddrOf[hostname] = ipaddr
		result[ipaddr] = nil
		rtt[ipaddr] = ringbuffer.NewRingBuffer(10)

		if hl := len(hostname); hl > layout.hostnameL {
			layout.hostnameL = hl
		}
	}
	layout.resultL = 2 + layout.hostnameL + 1 + 5 + 2 + 5 + 1
	layout.resultH = int(math.Ceil(float64(len(hostnames)) / 2.0))

	onRecv := make(chan *response)
	p.OnRecv = func(addr *net.IPAddr, t time.Duration) {
		onRecv <- &response{addr: addr, rtt: t}
	}

	onIdle := make(chan bool)
	p.OnIdle = func() {
		onIdle <- true
	}

	err := termbox.Init()
	if err != nil {
		panic(err)
	}
	defer termbox.Close()
	layout.termW, layout.termH = termbox.Size()

	layout.failedHistoryY = 1 + layout.resultH + 1 + 1

	histSize := layout.termH - layout.failedHistoryY - 2
	if histSize <= 0 {
		histSize = 10
	}
	failedHistory := ringbuffer.NewRingBuffer(histSize)

	drawHeader()
	_ = termbox.Flush()

	keyCh := make(chan termbox.Key)
	go keyEventLoop(keyCh)

	p.RunLoop()
	defer p.Stop()

loop:
	for {
		select {
		case res := <-onRecv:
			result[res.addr.String()] = res
		case <-onIdle:
			y := 2

			for i, hostname := range hostnames {
				ipaddr := ipaddrOf[hostname]
				res := result[ipaddr]
				var msg string
				var fg termbox.Attribute
				var st string
				if res != nil {
					st = "✓"
					fg = termbox.ColorGreen

					_ = rtt[ipaddr].Push(res.rtt)
					rttHist, _ := rtt[ipaddr].Fetch()
					var rttSum float64
					for _, t := range rttHist {
						rttSum += t.(time.Duration).Seconds()
					}
					avg := rttSum / float64(len(rttHist))

					msg = fmt.Sprintf("%-[1]*s %5.2f (%5.2f)", layout.hostnameL, hostname, res.rtt.Seconds()*1000, avg*1000)
				} else {
					st = "x"
					fg = termbox.ColorYellow

					msg = fmt.Sprintf("%-[1]*s", layout.resultL, hostname)
					faillog := fmt.Sprintf("%s %-24s",
						time.Now().Format("2006-01-02 15:04:05.000"),
						hostname)
					_ = failedHistory.Push(faillog)
 
				}
				if i%2 == 0 {
					drawColorfulStr(0, y, st, fg, termbox.ColorDefault)
					drawStr(2, y, msg)
				} else {
					drawStr(layout.resultL, y, " - ")
					drawColorfulStr(layout.resultL+3, y, st, fg, termbox.ColorDefault)
					drawStr(layout.resultL+5, y, msg)
					y++
				}

				result[ipaddr] = nil
			}
 
			_ = termbox.Flush()
		case key := <-keyCh:
			switch key {
			case termbox.KeyEsc, termbox.KeyCtrlC:
				break loop
			}
		case <-p.Done():
			if err = p.Err(); err != nil {
				panic(err)
			}
			break loop
		}
	}
}
