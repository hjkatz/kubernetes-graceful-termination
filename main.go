package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type State string

const (
	StateStartup  State = "startup"
	StateRunning  State = "running"
	StateShutdown State = "shutdown"
)

type Global struct {
	mu    sync.Mutex
	State State
}

// global variables for demonstration purposes only...
var (
	_global       Global
	_server       *http.Server
	_startupOnce  sync.Once
	_shutdownOnce sync.Once
)

func init() {
	_global = Global{
		// initial state
		State: StateStartup,
	}
}

func main() {
	// Example function to run forever, print the global state, and move between states
	for {
		printGlobalState()

		switch _global.State {
		case StateStartup:
			// use go routine to showcase states
			_startupOnce.Do(func() { go startup() })
		case StateRunning:
			// nothing to do
			// pretend to serve web requests or a heartbeat or something
		case StateShutdown:
			// use go routine to showcase states
			_shutdownOnce.Do(func() { go shutdown() })
		default:
			panic("unknown state: " + _global.State)
		}

		time.Sleep(1 * time.Second)
	}
}

func startup() {
	log.Println("Starting up...")
	go waitForShutdown()
	go startWebserver()

	// move to running
	setGlobalState(StateRunning)
}

func shutdown() {
	log.Println("Shutting down...")

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Second))
	drainConnections(ctx)
	stopWebserver(ctx)

	// wait for deadline
	<-ctx.Done()

	// ensure everything is stopped
	cancel()

	// all done
	log.Println("Exiting...")
	os.Exit(0)
}

func waitForShutdown() {
	log.Println("Setting up shutdown signal handler...")
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// wait
	sig := <-sigChan
	log.Println("Received signal: ", sig)

	setGlobalState(StateShutdown)
}

// Example function to simulate draining connections during shutdown at different rates
func drainConnections(ctx context.Context) {
	log.Println("Draining connections...")

	for i := 0; i <= 3; i++ {
		time.Sleep(1 * time.Second)
		log.Printf("Draining conn: %d", i)
	}
}

func startWebserver() {
	_server = &http.Server{
		Addr:    ":8080",
		Handler: http.DefaultServeMux,
	}

	// start our web service
	log.Println("Serving requests on :", _server.Addr)

	err := _server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("error when running web server: %s\n", err)
	}
}

func stopWebserver(ctx context.Context) {
	err := _server.Shutdown(ctx)
	if err != nil {
		log.Fatalf("failed to shutdown server: %s\n", err)
	}
}

func printGlobalState() {
	_global.mu.Lock()
	defer _global.mu.Unlock()
	log.Printf("State: %s\n", _global.State)
}

func setGlobalState(nextState State) {
	_global.mu.Lock()
	defer _global.mu.Unlock()
	log.Printf("Setting state from '%s' -> '%s'", _global.State, nextState)
	_global.State = nextState
}
