// Command probe sends one request to the gateway over HTTP or gRPC and prints the JSON answer.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/consultantstanleymn/inference-gateway/gen/inferencev1"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: probe http|grpc -addr host:port -text \"...\"")
		os.Exit(2)
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8080", "host:port")
	text := fs.String("text", "My card has not arrived yet", "message to classify")
	_ = fs.Parse(os.Args[2:])
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch os.Args[1] {
	case "http":
		body, _ := json.Marshal(map[string]string{"text": *text})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+*addr+"/v1/classify", bytes.NewReader(body)) //nolint:gosec // probe is an operator tool; the address is its own flag
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req) //nolint:gosec // probe is an operator tool; the address is its own flag
		if err != nil {
			fail(err)
		}
		defer func() { _ = resp.Body.Close() }()
		out, _ := io.ReadAll(resp.Body)
		fmt.Printf("%s", out)
		if resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
	case "grpc":
		conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			fail(err)
		}
		defer func() { _ = conn.Close() }()
		out, err := inferencev1.NewGatewayClient(conn).Classify(ctx, &inferencev1.ClassifyRequest{Text: *text})
		if err != nil {
			fail(err)
		}
		fmt.Println(protojson.Format(out))
	default:
		fail(fmt.Errorf("unknown transport %q", os.Args[1]))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
