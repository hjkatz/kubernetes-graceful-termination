package main

import (
	"context"
	"errors"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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
	_k8s          kubernetes.Interface
)

func init() {
	_global = Global{
		// initial state
		State: StateStartup,
	}
}

func main() {
	// Example function to run forever, print the global state, and move between states
	for range time.Tick(1 * time.Second) {
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
	}
}

func startup() {
	log.Println("Starting up...")
	setupK8sClient()
	go waitForShutdown()
	go startWebserver()

	// move to running
	setGlobalState(StateRunning)
}

func shutdown() {
	log.Println("Shutting down...")

	deadline := calculateShutdownDeadline()
	log.Printf("Server must shutdown before deadline: %s", deadline)

	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	go drainConnections(ctx)
	go stopWebserver(ctx)

	// wait for deadline
	<-ctx.Done()

	// ensure everything is stopped
	cancel()

	// wait a moment for go routines to finish any cleanup
	time.Sleep(500 * time.Millisecond)

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

func calculateShutdownDeadline() time.Time {
	if !isRunningInsideKubernetesPod() {
		defaultDeadline := time.Now().Add(10 * time.Second)
		log.Printf("Running outside kubernetes, setting shutdown deadline to default: %s", defaultDeadline)
		return defaultDeadline
	}

	log.Printf("Running inside kubernetes, polling Pod metadata to calculate deadline")
	deletionTime, gracePeriod := getPodDeletionInfo()

	log.Printf("Pod is expected to be deleted in %s at %s\n", gracePeriod, deletionTime)

	// choose some jitter time between min(5% of gracePeriod, 10s)
	// this time represents however long we think it may maximally take
	// our program to reach this point since receiving SIGTERM
	fivePercentSeconds := int(gracePeriod.Seconds() / 100 * 5)
	floorSeconds := 10
	jitterSeconds := floorSeconds
	if fivePercentSeconds > floorSeconds {
		jitterSeconds = fivePercentSeconds
	}
	if time.Until(deletionTime).Seconds() < float64(jitterSeconds) {
		log.Printf("WARN: jitter time longer than remaining gracePeriod, defaulting to 0\n")
		jitterSeconds = 0
	}
	log.Printf("calculated jitter of %d\n", jitterSeconds)

	// calculate deadline
	deadline := deletionTime.Add(-(time.Second * time.Duration(jitterSeconds)))
	return deadline
}

// getPodDeletionInfo polls the k8s api for $POD_NAME in $POD_NAMESPACE and
// returns `.metadata.deletionTimestamp`, `.metadata.deletionGracePeriodSeconds`
func getPodDeletionInfo() (time.Time, time.Duration) {
	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("POD_NAMESPACE")

	maxAttempts := 5
	attempt := 0

	for attempt <= maxAttempts {
		attempt++

		pod, err := _k8s.CoreV1().Pods(podNamespace).Get(context.TODO(), podName, v1.GetOptions{})
		if err != nil {
			log.Printf("Error while fetching Pod deletion info: %s\n", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		deletionTimestamp := pod.ObjectMeta.DeletionTimestamp
		deletionGracePeriodSeconds := pod.ObjectMeta.DeletionGracePeriodSeconds

		if deletionTimestamp != nil && deletionGracePeriodSeconds != nil {
			return deletionTimestamp.Time, time.Duration(*deletionGracePeriodSeconds) * time.Second
		}

		// backoff
		log.Println("Fetched Pod metadata, but deletion info was still empty!")
		time.Sleep(500 * time.Millisecond)
	}

	// some default
	log.Println("Failed to fetch Pod deletion info in a timely manner, returning default")
	return time.Now(), 20 * time.Second
}

// Example function to simulate draining connections during shutdown at different rates
func drainConnections(ctx context.Context) {
	deadline, _ := ctx.Deadline()
	timeRemaining := time.Until(deadline)
	activeConnections := 1000
	batchSize := int(float64(activeConnections) / math.Max(timeRemaining.Seconds()-1, 1.0))
	maxBatchSize := 100 // per second

	if batchSize > maxBatchSize {
		log.Printf("Calculated batchSize of %d is too large, using default size of %d per second instead\n", batchSize, maxBatchSize)
		batchSize = maxBatchSize
	}

	log.Printf("Draining %d connections in batches of %d over %s\n", activeConnections, batchSize, timeRemaining)

	go func() {
		<-ctx.Done()
		if activeConnections > 0 {
			log.Fatalf("Unable to safely shutdown in time, dropping %d connections on the floor...\n", activeConnections)
		}
	}()

	for activeConnections > 0 {
		activeConnections -= batchSize
		// real connections wouldn't be negative
		if activeConnections < 0 {
			activeConnections = 0
		}

		if activeConnections <= 0 {
			break
		}

		log.Printf("Draining %d connections, %d left\n", batchSize, activeConnections)

		time.Sleep(1 * time.Second)
	}

	log.Printf("Successfully drained all connections\n")
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

func isRunningInsideKubernetesPod() bool {
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

func setupK8sClient() {
	if !isRunningInsideKubernetesPod() {
		log.Println("Not running inside kubernetes, skipping setupK8sClient...")
		return
	}

	log.Print("Running inside kubernetes, setting up k8s client...")

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	_k8s, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
}
