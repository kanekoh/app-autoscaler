package main_test

import (
	"autoscaler/pruner"
	"io/ioutil"
	"os"
	"time"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/consuladapter"
	"code.cloudfoundry.org/lager/lagertest"
	"code.cloudfoundry.org/locket"
	"github.com/hashicorp/consul/api"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gexec"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
)

var _ = Describe("Pruner", func() {

	var (
		runner       *PrunerRunner
		consulClient consuladapter.Client
	)
	BeforeEach(func() {
		initConfig()
		consulRunner.Reset()
		consulClient = consulRunner.NewClient()
		runner = NewPrunerRunner()
	})

	AfterEach(func() {
		runner.KillWithFire()
	})

	Context("when the pruner acquires the lock", func() {
		BeforeEach(func() {
			runner.startCheck = ""
			runner.Start()
			Eventually(runner.Session.Buffer, 2*time.Second).Should(Say(runner.acquiredLockCheck))
		})

		It("registers itself with consul", func() {
			Eventually(runner.Session.Buffer, 2*time.Second).Should(Say("pruner.registration-runner.succeeded-registering-service"))

			services, err := consulClient.Agent().Services()
			Expect(err).ToNot(HaveOccurred())

			Expect(services).To(HaveKeyWithValue("pruner",
				&api.AgentService{
					Service: "pruner",
					ID:      "pruner",
				}))
		})

		It("registers a TTL healthcheck", func() {
			Eventually(runner.Session.Buffer, 2*time.Second).Should(Say("pruner.registration-runner.succeeded-registering-service"))

			checks, err := consulClient.Agent().Checks()
			Expect(err).ToNot(HaveOccurred())

			Expect(checks).To(HaveKeyWithValue("service:pruner",
				&api.AgentCheck{
					Node:        "0",
					CheckID:     "service:pruner",
					Name:        "Service 'pruner' check",
					Status:      "passing",
					ServiceID:   "pruner",
					ServiceName: "pruner",
				}))
		})

		It("should start", func() {
			Eventually(runner.Session.Buffer, 2*time.Second).Should(Say("pruner.instancemetrics-dbpruner.started"))
			Eventually(runner.Session.Buffer, 2*time.Second).Should(Say("pruner.appmetrics-dbpruner.started"))
			Eventually(runner.Session.Buffer, 2*time.Second).Should(Say("pruner.scalingengine-dbpruner.started"))
			Eventually(runner.Session.Buffer, 2*time.Second).Should(Say("pruner.started"))
			Consistently(runner.Session).ShouldNot(Exit())
		})
	})

	Context("when the pruner loses the lock", func() {
		BeforeEach(func() {
			runner.startCheck = ""
			runner.Start()

			Eventually(runner.Session.Buffer, 2*time.Second).Should(Say(runner.acquiredLockCheck))
			Eventually(runner.Session.Buffer, 2*time.Second).Should(Say("pruner.started"))

			consulRunner.Reset()
		})

		It("exits with failure", func() {
			Eventually(runner.Session, 4*time.Second).Should(Exit(1))
			Expect(runner.Session.Buffer()).Should(Say("exited-with-failure"))
		})
	})

	Context("when the pruner initially does not have the lock", func() {
		var competingPrunerProcess ifrit.Process

		BeforeEach(func() {
			logger := lagertest.NewTestLogger("competing-process")
			buffer := logger.Buffer()

			competingPrunerLock := locket.NewLock(logger, consulClient, pruner.PrunerLockSchemaPath(), []byte{}, clock.NewClock(), cfg.Lock.LockRetryInterval, cfg.Lock.LockTTL)
			competingPrunerProcess = ifrit.Invoke(competingPrunerLock)
			Eventually(buffer, 2*time.Second).Should(Say("competing-process.lock.acquire-lock-succeeded"))

			runner.startCheck = ""
			runner.Start()
		})

		It("should not start", func() {
			Eventually(runner.Session.Buffer).Should(Say("pruner.lock.acquiring-lock"))
			Consistently(runner.Session.Buffer).ShouldNot(Say("pruner.started"))
		})

		Describe("when the lock becomes available", func() {
			BeforeEach(func() {
				ginkgomon.Kill(competingPrunerProcess)
			})

			It("acquires the lock and starts", func() {
				Eventually(runner.Session.Buffer, 2*time.Second).Should(Say(runner.acquiredLockCheck))
				Eventually(runner.Session.Buffer, 2*time.Second).Should(Say("pruner.instancemetrics-dbpruner.started"))
				Eventually(runner.Session.Buffer, 2*time.Second).Should(Say("pruner.appmetrics-dbpruner.started"))
				Eventually(runner.Session.Buffer, 2*time.Second).Should(Say("pruner.scalingengine-dbpruner.started"))
				Eventually(runner.Session.Buffer, 2*time.Second).Should(Say("pruner.started"))
				Consistently(runner.Session).ShouldNot(Exit())
			})

		})
	})

	Context("with a missing config file", func() {
		BeforeEach(func() {
			runner.configPath = "bogus"
			runner.Start()
		})

		It("fails with an error", func() {
			Eventually(runner.Session).Should(Exit(1))
			Expect(runner.Session.Buffer()).To(Say("failed to open config file"))
		})
	})

	Context("with an invalid config file", func() {
		BeforeEach(func() {
			badfile, err := ioutil.TempFile("", "bad-pr-config")
			Expect(err).NotTo(HaveOccurred())
			runner.configPath = badfile.Name()
			ioutil.WriteFile(runner.configPath, []byte("bogus"), os.ModePerm)
			runner.Start()
		})

		AfterEach(func() {
			os.Remove(runner.configPath)
		})

		It("fails with an error", func() {
			Eventually(runner.Session).Should(Exit(1))
			Expect(runner.Session.Buffer()).To(Say("failed to read config file"))
		})
	})

	Context("with missing/invalid configuration", func() {
		BeforeEach(func() {

			cfg.InstanceMetricsDb.CutoffDays = -1

			cfg := writeConfig(&cfg)
			runner.configPath = cfg.Name()
			runner.Start()
		})

		AfterEach(func() {
			os.Remove(runner.configPath)
		})

		It("should fail validation", func() {
			Eventually(runner.Session).Should(Exit(1))
			Expect(runner.Session.Buffer()).To(Say("failed to validate configuration"))
		})
	})

	Context("when connection to instancemetrics db fails", func() {
		BeforeEach(func() {
			cfg.InstanceMetricsDb.DbUrl = "postgres://not-exist-user:not-exist-password@localhost/autoscaler?sslmode=disable"
			cfg := writeConfig(&cfg)
			runner.configPath = cfg.Name()
			runner.Start()
		})

		AfterEach(func() {
			os.Remove(runner.configPath)
		})

		It("should error", func() {
			Eventually(runner.Session).Should(Exit(1))
			Expect(runner.Session.Buffer()).To(Say("failed to connect instancemetrics db"))
		})

	})

	Context("when connection to appmetrics db fails", func() {
		BeforeEach(func() {
			cfg.AppMetricsDb.DbUrl = "postgres://not-exist-user:not-exist-password@localhost/autoscaler?sslmode=disable"
			cfg := writeConfig(&cfg)
			runner.configPath = cfg.Name()
			runner.Start()
		})

		AfterEach(func() {
			os.Remove(runner.configPath)
		})

		It("should error", func() {
			Eventually(runner.Session).Should(Exit(1))
			Expect(runner.Session.Buffer()).To(Say("failed to connect appmetrics db"))
		})

	})

	Context("when connection to scalingengine db fails", func() {
		BeforeEach(func() {
			cfg.ScalingEngineDb.DbUrl = "postgres://not-exist-user:not-exist-password@localhost/autoscaler?sslmode=disable"
			cfg := writeConfig(&cfg)
			runner.configPath = cfg.Name()
			runner.Start()
		})

		AfterEach(func() {
			os.Remove(runner.configPath)
		})

		It("should error", func() {
			Eventually(runner.Session).Should(Exit(1))
			Expect(runner.Session.Buffer()).To(Say("failed to connect scalingengine db"))
		})

	})

	Context("when an interrupt is sent", func() {
		BeforeEach(func() {
			runner.Start()
		})

		It("should stop", func() {
			runner.Interrupt()
			Eventually(runner.Session, 5).Should(Exit(130))
		})
	})
})
