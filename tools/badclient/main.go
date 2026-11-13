// Command badclient misbehaves against the matcher to check that other clients do not suffer
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"flag"
	"log"
	"net"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	pb "github.com/Aset-Mk/geomatch/internal/pb/matcherv1"
)

func main() {
	addr := flag.String("addr", "localhost:50051", "matcher address")
	mode := flag.String("mode", "", "stall or slowread")
	n := flag.Int("n", 10000, "stall: streams to open")
	conns := flag.Int("conns", 100, "stall: connections to spread the streams over")
	rate := flag.Int("rate", 10000, "slowread: requests per second, in steps of 1000")
	flag.Parse()

	var err error
	switch *mode {
	case "stall":
		err = stall(*addr, *n, *conns)
	case "slowread":
		err = slowread(*addr, *rate)
	default:
		log.Fatal("mode must be stall or slowread")
	}
	log.Fatal(err)
}

// stall opens n update streams, sends one driver on each and keeps them open and silent
func stall(addr string, n, conns int) error {
	cs := make([]pb.MatcherClient, conns)
	for i := range cs {
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return err
		}
		cs[i] = pb.NewMatcherClient(conn)
	}
	for i := range n {
		st, err := cs[i%conns].UpdateDrivers(context.Background())
		if err != nil {
			return err
		}
		// ids far above the ones loadgen uses
		err = st.Send(&pb.UpdateDriversRequest{Updates: []*pb.DriverUpdate{{
			DriverId: uint64(1_000_000 + i),
			Pos:      &pb.Point{Lat: 55.7, Lon: 37.6},
		}}})
		if err != nil {
			return err
		}
	}
	log.Printf("%d streams open", n)
	select {}
}

// slowread sends Nearest requests over raw HTTP/2 and never reads the answers, a grpc client would read them
func slowread(addr string, rate int) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	if _, err := conn.Write([]byte(http2.ClientPreface)); err != nil {
		return err
	}
	fr := http2.NewFramer(conn, conn)
	if err := fr.WriteSettings(); err != nil {
		return err
	}

	body, err := proto.Marshal(&pb.NearestRequest{Point: &pb.Point{Lat: 55.75, Lon: 37.6}, K: 100})
	if err != nil {
		return err
	}
	// a grpc message is a zero byte, the length and the body
	msg := make([]byte, 5+len(body))
	binary.BigEndian.PutUint32(msg[1:], uint32(len(body)))
	copy(msg[5:], body)

	var hb bytes.Buffer
	enc := hpack.NewEncoder(&hb)
	tick := time.NewTicker(time.Millisecond)
	for id := uint32(1); ; {
		<-tick.C
		for range rate / 1000 {
			hb.Reset()
			for _, f := range [][2]string{
				{":method", "POST"}, {":scheme", "http"}, {":path", pb.Matcher_Nearest_FullMethodName},
				{":authority", addr}, {"content-type", "application/grpc"}, {"te", "trailers"},
			} {
				if err := enc.WriteField(hpack.HeaderField{Name: f[0], Value: f[1]}); err != nil {
					return err
				}
			}
			if err := fr.WriteHeaders(http2.HeadersFrameParam{StreamID: id, BlockFragment: hb.Bytes(), EndHeaders: true}); err != nil {
				return err
			}
			if err := fr.WriteData(id, true, msg); err != nil {
				return err
			}
			id += 2
		}
	}
}
