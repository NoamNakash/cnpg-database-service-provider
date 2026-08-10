package config_test

import (
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/cnpg-database-service-provider/internal/config"
)

var _ = Describe("Configuration", func() {
	clearEnv := func() {
		_ = os.Unsetenv("SP_SERVER_ADDRESS")
		_ = os.Unsetenv("SP_SERVER_SHUTDOWN_TIMEOUT")
		_ = os.Unsetenv("SP_SERVER_READ_TIMEOUT")
		_ = os.Unsetenv("SP_SERVER_WRITE_TIMEOUT")
		_ = os.Unsetenv("SP_SERVER_IDLE_TIMEOUT")
		_ = os.Unsetenv("SP_SERVER_REQUEST_TIMEOUT")
		_ = os.Unsetenv("SP_NAME")
		_ = os.Unsetenv("SP_DISPLAY_NAME")
		_ = os.Unsetenv("SP_ENDPOINT")
		_ = os.Unsetenv("SP_REGION")
		_ = os.Unsetenv("SP_ZONE")
		_ = os.Unsetenv("DCM_REGISTRATION_URL")
		_ = os.Unsetenv("SP_NATS_URL")
		_ = os.Unsetenv("SP_K8S_NAMESPACE")
		_ = os.Unsetenv("SP_K8S_KUBECONFIG")
		_ = os.Unsetenv("SP_K8S_DEFAULT_STORAGE_CLASS")
		_ = os.Unsetenv("SP_K8S_IMAGE_CATALOG")
		_ = os.Unsetenv("SP_K8S_EXTERNAL_SVC_TYPE")
	}

	BeforeEach(func() {
		clearEnv()
	})

	AfterEach(func() {
		clearEnv()
	})

	setRequiredEnv := func() {
		_ = os.Setenv("SP_NAME", "test-sp")
		_ = os.Setenv("SP_ENDPOINT", "https://test.example.com")
		_ = os.Setenv("DCM_REGISTRATION_URL", "https://dcm.example.com")
		_ = os.Setenv("SP_NATS_URL", "nats://test:4222")
		_ = os.Setenv("SP_K8S_EXTERNAL_SVC_TYPE", "LoadBalancer")
	}

	It("loads configuration from environment vaiables", func() {
		setRequiredEnv()
		_ = os.Setenv("SP_SERVER_ADDRESS", ":9000")
		_ = os.Setenv("SP_K8S_DEFAULT_STORAGE_CLASS", "az-a")
		_ = os.Setenv("SP_K8S_IMAGE_CATALOG", "psql-images")
		_ = os.Setenv("SP_SERVER_SHUTDOWN_TIMEOUT", "30s")
		_ = os.Setenv("SP_DISPLAY_NAME", "Test Provider")
		_ = os.Setenv("SP_REGION", "us-east-1")
		_ = os.Setenv("SP_ZONE", "us-east-1a")
		_ = os.Setenv("SP_SERVER_READ_TIMEOUT", "10s")
		_ = os.Setenv("SP_SERVER_WRITE_TIMEOUT", "20s")
		_ = os.Setenv("SP_SERVER_IDLE_TIMEOUT", "120s")
		_ = os.Setenv("SP_SERVER_REQUEST_TIMEOUT", "45s")

		cfg, err := config.Load()
		Expect(err).NotTo(HaveOccurred())

		Expect(cfg.Server.Address).To(Equal(":9000"))
		Expect(cfg.Server.ShutdownTimeout).To(Equal(30 * time.Second))
		Expect(cfg.Server.ReadTimeout).To(Equal(10 * time.Second))
		Expect(cfg.Server.WriteTimeout).To(Equal(20 * time.Second))
		Expect(cfg.Server.IdleTimeout).To(Equal(120 * time.Second))
		Expect(cfg.Server.RequestTimeout).To(Equal(45 * time.Second))

		Expect(cfg.Provider.Name).To(Equal("test-sp"))
		Expect(cfg.Provider.DisplayName).To(Equal("Test Provider"))
		Expect(cfg.Provider.Endpoint).To(Equal("https://test.example.com"))
		Expect(cfg.Provider.Region).To(Equal("us-east-1"))
		Expect(cfg.Provider.Zone).To(Equal("us-east-1a"))

		Expect(cfg.DCM.RegistrationURL).To(Equal("https://dcm.example.com"))

		Expect(cfg.NATS.URL).To(Equal("nats://test:4222"))

		Expect(cfg.Kubernetes.Namespace).To(Equal("default"))
		Expect(cfg.Kubernetes.DefaultStorageClass).To(Equal("az-a"))
		Expect(cfg.Kubernetes.ImageCatalog).To(Equal("psql-images"))
		Expect(cfg.Kubernetes.ExternalServiceType).To(Equal("LoadBalancer"))
	})

	It("applies default values when no config is specified", func() {
		setRequiredEnv()

		cfg, err := config.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())

		Expect(cfg.Server.Address).To(Equal(":8080"))
		Expect(cfg.Server.ShutdownTimeout).To(Equal(15 * time.Second))
		Expect(cfg.Server.ReadTimeout).To(Equal(15 * time.Second))
		Expect(cfg.Server.WriteTimeout).To(Equal(15 * time.Second))
		Expect(cfg.Server.IdleTimeout).To(Equal(60 * time.Second))
		Expect(cfg.Server.RequestTimeout).To(Equal(30 * time.Second))

		Expect(cfg.Kubernetes.Namespace).To(Equal("default"))

		Expect(cfg.Monitoring.DebounceMs).To(Equal(500))
		Expect(cfg.Monitoring.ResyncPeriod).To(Equal(10 * time.Minute))
	})

	It("returns error when required fields are missing", func() {
		cfg, err := config.Load()
		Expect(err).To(HaveOccurred())
		Expect(cfg).To(BeNil())

		errMsg := err.Error()
		Expect(errMsg).To(HavePrefix("loading configuration:"))
		Expect(errMsg).To(ContainSubstring("SP_NAME"))
		Expect(errMsg).To(ContainSubstring("SP_ENDPOINT"))
		Expect(errMsg).To(ContainSubstring("DCM_REGISTRATION_URL"))
		Expect(errMsg).To(ContainSubstring("SP_NATS_URL"))
	})

	It("returns error when SP_K8S_EXTERNAL_SVC_TYPE is not set", func() {
		setRequiredEnv()
		_ = os.Unsetenv("SP_K8S_EXTERNAL_SVC_TYPE")

		cfg, err := config.Load()
		Expect(err).To(HaveOccurred())
		Expect(cfg).To(BeNil())

		Expect(err.Error()).To(ContainSubstring("loading configuration: invalid SP_K8S_EXTERNAL_SVC_TYPE"))
		Expect(err.Error()).To(ContainSubstring("must be LoadBalancer or NodePort"))
	})

	It("loads successfully with ExternalServiceType=LoadBalancer", func() {
		setRequiredEnv()
		_ = os.Setenv("SP_K8S_EXTERNAL_SVC_TYPE", "LoadBalancer")

		cfg, err := config.Load()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Kubernetes.ExternalServiceType).To(Equal("LoadBalancer"))
	})

	It("loads successfully with ExternalServiceType=NodePort", func() {
		setRequiredEnv()
		_ = os.Setenv("SP_K8S_EXTERNAL_SVC_TYPE", "NodePort")

		cfg, err := config.Load()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Kubernetes.ExternalServiceType).To(Equal("NodePort"))
	})
})
