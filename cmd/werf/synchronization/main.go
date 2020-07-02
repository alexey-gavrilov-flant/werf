package synchronization

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/werf/kubedog/pkg/kube"

	"github.com/werf/lockgate"

	"github.com/werf/werf/pkg/storage"
	"github.com/werf/werf/pkg/storage/synchronization_server"

	"github.com/spf13/cobra"

	"github.com/werf/werf/cmd/werf/common"
	"github.com/werf/werf/pkg/werf"
)

var cmdData struct {
	Kubernetes                bool
	KubernetesNamespacePrefix string

	Local                          bool
	LocalLockManagerBaseDir        string
	LocalStagesStorageCacheBaseDir string

	TTL  string
	Host string
	Port string
}

var commonCmdData common.CmdData

func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "synchronization",
		Short:                 "Run synchronization server",
		Long:                  common.GetLongCommandDescription(`Run synchronization server`),
		DisableFlagsInUseLine: true,
		Annotations:           map[string]string{},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := common.ProcessLogOptions(&commonCmdData); err != nil {
				common.PrintHelp(cmd)
				return err
			}

			common.LogVersion()

			return common.LogRunningTime(func() error {
				return runSynchronization()
			})
		},
	}

	common.SetupTmpDir(&commonCmdData, cmd)
	common.SetupHomeDir(&commonCmdData, cmd)

	common.SetupLogOptions(&commonCmdData, cmd)

	common.SetupKubeConfig(&commonCmdData, cmd)
	common.SetupKubeContext(&commonCmdData, cmd)

	cmd.Flags().BoolVarP(&cmdData.Local, "local", "", common.GetBoolEnvironmentDefaultTrue("WERF_LOCAL"), "Use file lock-manager and file stages-storage-cache (true by default or $WERF_LOCAL)")
	cmd.Flags().StringVarP(&cmdData.LocalLockManagerBaseDir, "local-lock-manager-base-dir", "", os.Getenv("WERF_LOCAL_LOCK_MANAGER_BASE_DIR"), "Use specified directory as base for file lock-manager (~/.werf/synchronization_server/lock_manager by default or $WERF_LOCAL_LOCK_MANAGER_BASE_DIR)")
	cmd.Flags().StringVarP(&cmdData.LocalStagesStorageCacheBaseDir, "local-stages-storage-cache-base-dir", "", os.Getenv("WERF_LOCAL_STAGES_STORAGE_CACHE_BASE_DIR"), "Use specified directory as base for file stages-storage-cache (~/.werf/synchronization_server/stages_storage_cache by default or $WERF_LOCAL_STAGES_STORAGE_CACHE_BASE_DIR)")

	cmd.Flags().BoolVarP(&cmdData.Kubernetes, "kubernetes", "", common.GetBoolEnvironmentDefaultFalse("WERF_KUBERNETES"), "Use kubernetes lock-manager stages-storage-cache (default $WERF_KUBERNETES)")
	cmd.Flags().StringVarP(&cmdData.KubernetesNamespacePrefix, "kubernetes-namespace-prefix", "", os.Getenv("WERF_KUBERNETES_NAMESPACE_PREFIX"), "Use specified prefix for namespaces created for lock-manager and stages-storage-cache (defaults to 'werf-synchronization-' when --kubernetes option is used or $WERF_KUBERNETES_NAMESPACE_PREFIX)")

	cmd.Flags().StringVarP(&cmdData.TTL, "ttl", "", os.Getenv("WERF_TTL"), "Time to live for lock-manager locks and stages-storage-cache records (default $WERF_TTL)")
	cmd.Flags().StringVarP(&cmdData.Host, "host", "", os.Getenv("WERF_HOST"), "Bind synchronization server to the specified host (default localhost or $WERF_HOST)")
	cmd.Flags().StringVarP(&cmdData.Port, "port", "", os.Getenv("WERF_PORT"), "Bind synchronization server to the specified port (default 55581 or $WERF_PORT)")

	return cmd
}

func runSynchronization() error {
	if err := werf.Init(*commonCmdData.TmpDir, *commonCmdData.HomeDir); err != nil {
		return fmt.Errorf("initialization error: %s", err)
	}

	host, port := cmdData.Host, cmdData.Port
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "55581"
	}

	var lockManagerFactoryFunc func(clientID string) (storage.LockManager, error)
	var stagesStorageCacheFactoryFunc func(clientID string) (storage.StagesStorageCache, error)

	if cmdData.Kubernetes {
		if err := kube.Init(kube.InitOptions{KubeContext: *commonCmdData.KubeContext, KubeConfig: *commonCmdData.KubeConfig}); err != nil {
			return fmt.Errorf("cannot initialize kube: %s", err)
		}

		if err := common.InitKubedog(); err != nil {
			return fmt.Errorf("cannot init kubedog: %s", err)
		}

		prefix := cmdData.KubernetesNamespacePrefix
		if prefix == "" {
			prefix = "werf-synchronization-"
		}

		lockManagerFactoryFunc = func(clientID string) (storage.LockManager, error) {
			return storage.NewKubernetesLockManager(fmt.Sprintf("%s%s", prefix, clientID)), nil
		}

		stagesStorageCacheFactoryFunc = func(clientID string) (storage.StagesStorageCache, error) {
			return storage.NewKubernetesStagesStorageCache(fmt.Sprintf("%s%s", prefix, clientID)), nil
		}
	} else {
		lockManagerBaseDir := cmdData.LocalLockManagerBaseDir
		if lockManagerBaseDir == "" {
			lockManagerBaseDir = filepath.Join(werf.GetHomeDir(), "synchronization_server", "lock_manager")
		}

		stagesStorageCacheBaseDir := cmdData.LocalStagesStorageCacheBaseDir
		if stagesStorageCacheBaseDir == "" {
			stagesStorageCacheBaseDir = filepath.Join(werf.GetHomeDir(), "synchronization_server", "stages_storage_cache")
		}

		lockManagerFactoryFunc = func(clientID string) (storage.LockManager, error) {
			if locker, err := lockgate.NewFileLocker(filepath.Join(lockManagerBaseDir, clientID)); err != nil {
				return nil, err
			} else {
				return storage.NewGenericLockManager(locker), nil
			}
		}

		stagesStorageCacheFactoryFunc = func(clientID string) (storage.StagesStorageCache, error) {
			return storage.NewFileStagesStorageCache(filepath.Join(stagesStorageCacheBaseDir, clientID)), nil
		}
	}

	return synchronization_server.RunSynchronizationServer(host, port, lockManagerFactoryFunc, stagesStorageCacheFactoryFunc)
}
