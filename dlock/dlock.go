package dlock

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" // Load gcp auth provider.
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

type LockConfig struct {
	Namespace string
	Name      string
	ID        string

	DevKubeConfig string

	Logger *zap.SugaredLogger
}

func New(ctx context.Context, cfg *LockConfig) (func(), error) {
	var (
		kubeConfig *rest.Config
		id         = cfg.ID
	)
	switch {
	// If a local kube path is specified, build the config from there.
	case cfg.DevKubeConfig != "":
		var err error
		kubeConfig, err = clientcmd.BuildConfigFromFlags(
			"", cfg.DevKubeConfig,
		)
		if err != nil {
			return nil, err
		}

		// In dev mode, generate a unique id if not provided.
		if id == "" {
			id = uuid.New().String()
		}

	// If deployed, we get the kube config from env vars and files.
	default:
		var err error
		kubeConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	}

	logger := cfg.Logger.With("id", id, "namespace", cfg.Namespace,
		"name", cfg.Name)

	client, err := clientset.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      cfg.Name,
			Namespace: cfg.Namespace,
		},
		Client: client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}

	lockAcquired := make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		// Start the leader election code loop
		logger.Debugw("Acquiring lock")

		leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
			Lock: lock,
			// IMPORTANT: you MUST ensure that any code you have that
			// is protected by the lease must terminate **before**
			// you call cancel. Otherwise, you could have a background
			// loop still running and another process could
			// get elected before your background loop finished, violating
			// the stated goal of the lease.
			ReleaseOnCancel: true,
			LeaseDuration:   60 * time.Second,
			RenewDeadline:   15 * time.Second,
			RetryPeriod:     5 * time.Second,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(ctx context.Context) {
					close(lockAcquired)
				},
				OnStoppedLeading: func() {
					select {
					// We stopped leading because the
					// context was cancelled. This is a
					// normal exit.
					case <-ctx.Done():

					default:
						logger.Infow("Leader status lost, exiting")
						os.Exit(1)
					}

				},
				OnNewLeader: func(identity string) {
					// We're notified that a new leader
					// elected was elected.
					if identity == id {
						return
					}

					// If it's not us, log the new leader
					// identity.
					logger.Infow("New leader elected",
						"leaderId", identity)
				},
			},
		})
	}()

	select {
	case <-lockAcquired:
		logger.Debugw("Lock acquired")

		unlock := func() {
			cancel()

			// Wait for `leaderelection.RunOrDie` to terminate. Otherwise
			// the main thread my already terminate the process before the
			// lock is released.
			wg.Wait()

			logger.Infow("Lock released")
		}

		return unlock, nil

	case <-ctx.Done():
		wg.Wait()

		return nil, ctx.Err()
	}

}
