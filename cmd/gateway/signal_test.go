package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// TestSIGTERMDrains starts the real binary with the real model, sends SIGTERM while requests
// are running, and checks that readiness drops, requests that started still succeed, and the
// process exits with code 0.
func TestSIGTERMDrains(t *testing.T) {
	root := filepath.Join("..", "..")
	lib := filepath.Join(root, "third_party", "libonnxruntime.so")
	if _, err := os.Stat(lib); err != nil {
		t.Skip("libonnxruntime.so missing; run scripts/fetch-onnxruntime.sh")
	}
	bin := filepath.Join(t.TempDir(), "gateway")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	hp, gp := freePort(t), freePort(t)
	cmd := exec.Command(bin, "-model-dir", filepath.Join(root, "models", "v1"), "-ort-lib", lib,
		"-http-addr", fmt.Sprintf("127.0.0.1:%d", hp), "-grpc-addr", fmt.Sprintf("127.0.0.1:%d", gp),
		"-shutdown-delay", "600ms")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	base := fmt.Sprintf("http://127.0.0.1:%d", hp)
	deadline := time.Now().Add(30 * time.Second)
	for {
		if resp, err := http.Get(base + "/readyz"); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("gateway never became ready")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Traffic in flight while the signal arrives.
	results := make(chan int, 40)
	for range 40 {
		go func() {
			resp, err := http.Post(base+"/v1/classify", "application/json", strings.NewReader(`{"text":"I lost my card"}`))
			if err != nil {
				results <- -1
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			results <- resp.StatusCode
		}()
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	for range 40 {
		if code := <-results; code != 200 {
			t.Fatalf("request that started before SIGTERM got %d", code)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if resp, err := http.Get(base + "/readyz"); err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode != 503 {
			t.Fatalf("readyz after SIGTERM = %d, want 503", resp.StatusCode)
		}
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("exit: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("gateway did not exit")
	}
}
