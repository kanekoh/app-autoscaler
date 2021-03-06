package server_test

import (
	"autoscaler/metricscollector/fakes"
	. "autoscaler/metricscollector/server"
	"autoscaler/models"

	"code.cloudfoundry.org/lager"
	"github.com/cloudfoundry/sonde-go/events"
	"github.com/gogo/protobuf/proto"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
)

var testUrlMemoryMetricHistories = "http://localhost/v1/apps/an-app-id/metric_histories/memory"

var _ = Describe("MemoryMetricHandler", func() {

	var (
		cfc      *fakes.FakeCfClient
		consumer *fakes.FakeNoaaConsumer
		handler  *MemoryMetricHandler
		database *fakes.FakeInstanceMetricsDB

		resp *httptest.ResponseRecorder
		req  *http.Request
		err  error

		metric1 models.AppInstanceMetric
		metric2 models.AppInstanceMetric
	)

	BeforeEach(func() {
		cfc = &fakes.FakeCfClient{}
		consumer = &fakes.FakeNoaaConsumer{}
		logger := lager.NewLogger("handler-test")
		database = &fakes.FakeInstanceMetricsDB{}
		resp = httptest.NewRecorder()
		handler = NewMemoryMetricHandler(logger, cfc, consumer, database)
	})

	Describe("GetMemoryMetric", func() {
		JustBeforeEach(func() {
			handler.GetMemoryMetric(resp, nil, map[string]string{"appid": "an-app-id"})
		})

		Context("when retrieving container metrics fail", func() {
			BeforeEach(func() {
				consumer.ContainerEnvelopesReturns(nil, errors.New("an error"))
			})

			It("returns a 500", func() {
				Expect(resp.Code).To(Equal(http.StatusInternalServerError))

				errJson := &models.ErrorResponse{}
				err = json.Unmarshal(resp.Body.Bytes(), errJson)

				Expect(err).ToNot(HaveOccurred())
				Expect(errJson).To(Equal(&models.ErrorResponse{
					Code:    "Interal-Server-Error",
					Message: "Error getting memory metrics from doppler",
				}))
			})
		})

		Context("when retrieving container metrics succeeds", func() {
			Context("container metrics is not empty", func() {
				BeforeEach(func() {
					timestamp := int64(111111)
					consumer.ContainerEnvelopesReturns([]*events.Envelope{
						&events.Envelope{
							ContainerMetric: &events.ContainerMetric{
								ApplicationId: proto.String("an-app-id"),
								InstanceIndex: proto.Int32(0),
								MemoryBytes:   proto.Uint64(12345678),
							},
							Timestamp: &timestamp,
						},
						&events.Envelope{
							ContainerMetric: &events.ContainerMetric{
								ApplicationId: proto.String("an-app-id"),
								InstanceIndex: proto.Int32(1),
								MemoryBytes:   proto.Uint64(87654321),
							},
							Timestamp: &timestamp,
						},
					}, nil)
				})

				It("returns a 200 response with metrics", func() {
					Expect(resp.Code).To(Equal(http.StatusOK))

					metrics := []models.AppInstanceMetric{}
					err = json.Unmarshal(resp.Body.Bytes(), &metrics)

					Expect(err).ToNot(HaveOccurred())
					Expect(metrics).To(HaveLen(2))

					Expect(metrics[0]).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"AppId":         Equal("an-app-id"),
						"InstanceIndex": BeEquivalentTo(0),
						"Name":          Equal(models.MetricNameMemory),
						"Unit":          Equal(models.UnitMegaBytes),
						"Value":         Equal("12"),
						"Timestamp":     BeEquivalentTo(111111),
					}))

					Expect(metrics[1]).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
						"AppId":         Equal("an-app-id"),
						"InstanceIndex": BeEquivalentTo(1),
						"Name":          Equal(models.MetricNameMemory),
						"Unit":          Equal(models.UnitMegaBytes),
						"Value":         Equal("84"),
						"Timestamp":     BeEquivalentTo(111111),
					}))

				})
			})

			Context("container metrics is empty", func() {
				BeforeEach(func() {
					consumer.ContainerEnvelopesReturns([]*events.Envelope{}, nil)
				})

				It("returns a 200 with empty metrics", func() {
					Expect(resp.Code).To(Equal(http.StatusOK))

					metrics := []models.AppInstanceMetric{}
					err = json.Unmarshal(resp.Body.Bytes(), &metrics)

					Expect(err).ToNot(HaveOccurred())
					Expect(metrics).To(BeEmpty())
				})
			})

		})
	})

	Describe("GetMemoryMetricHistory", func() {
		JustBeforeEach(func() {
			handler.GetMemoryMetricHistories(resp, req, map[string]string{"appid": "an-app-id"})
		})

		Context("when request query string is invalid", func() {
			Context("when there are multiple start pararmeters in query string", func() {
				BeforeEach(func() {
					req, err = http.NewRequest(http.MethodGet, testUrlMemoryMetricHistories+"?start=123&start=231", nil)
					Expect(err).ToNot(HaveOccurred())

				})

				It("returns 400", func() {
					Expect(resp.Code).To(Equal(http.StatusBadRequest))

					errJson := &models.ErrorResponse{}
					err = json.Unmarshal(resp.Body.Bytes(), errJson)

					Expect(err).ToNot(HaveOccurred())
					Expect(errJson).To(Equal(&models.ErrorResponse{
						Code:    "Bad-Request",
						Message: "Incorrect start parameter in query string",
					}))
				})
			})

			Context("when start time is not a number", func() {
				BeforeEach(func() {
					req, err = http.NewRequest(http.MethodGet, testUrlMemoryMetricHistories+"?start=abc", nil)
					Expect(err).ToNot(HaveOccurred())

				})

				It("returns 400", func() {
					Expect(resp.Code).To(Equal(http.StatusBadRequest))

					errJson := &models.ErrorResponse{}
					err = json.Unmarshal(resp.Body.Bytes(), errJson)

					Expect(err).ToNot(HaveOccurred())
					Expect(errJson).To(Equal(&models.ErrorResponse{
						Code:    "Bad-Request",
						Message: "Error parsing start time",
					}))
				})
			})

			Context("when there are multiple end parameters in query string", func() {
				BeforeEach(func() {
					req, err = http.NewRequest(http.MethodGet, testUrlMemoryMetricHistories+"?end=123&end=231", nil)
					Expect(err).ToNot(HaveOccurred())

				})

				It("returns 400", func() {
					Expect(resp.Code).To(Equal(http.StatusBadRequest))

					errJson := &models.ErrorResponse{}
					err = json.Unmarshal(resp.Body.Bytes(), errJson)

					Expect(err).ToNot(HaveOccurred())
					Expect(errJson).To(Equal(&models.ErrorResponse{
						Code:    "Bad-Request",
						Message: "Incorrect end parameter in query string",
					}))
				})
			})

			Context("when end time is not a number", func() {
				BeforeEach(func() {
					req, err = http.NewRequest(http.MethodGet, testUrlMemoryMetricHistories+"?end=abc", nil)
					Expect(err).ToNot(HaveOccurred())

				})

				It("returns 400", func() {
					Expect(resp.Code).To(Equal(http.StatusBadRequest))

					errJson := &models.ErrorResponse{}
					err = json.Unmarshal(resp.Body.Bytes(), errJson)

					Expect(err).ToNot(HaveOccurred())
					Expect(errJson).To(Equal(&models.ErrorResponse{
						Code:    "Bad-Request",
						Message: "Error parsing end time",
					}))
				})
			})

		})

		Context("when request query string is valid", func() {
			Context("when there are both start and end time in query string", func() {
				BeforeEach(func() {
					req, err = http.NewRequest(http.MethodGet, testUrlMemoryMetricHistories+"?start=123&end=567", nil)
					Expect(err).ToNot(HaveOccurred())
				})

				It("queries metrics from database with the given start and end time ", func() {
					appid, name, start, end := database.RetrieveInstanceMetricsArgsForCall(0)
					Expect(appid).To(Equal("an-app-id"))
					Expect(name).To(Equal(models.MetricNameMemory))
					Expect(start).To(Equal(int64(123)))
					Expect(end).To(Equal(int64(567)))
				})

			})

			Context("when there is no start time in query string", func() {
				BeforeEach(func() {
					req, err = http.NewRequest(http.MethodGet, testUrlMemoryMetricHistories+"?end=123", nil)
					Expect(err).ToNot(HaveOccurred())
				})

				It("queries metrics from database with start time  0", func() {
					_, _, start, _ := database.RetrieveInstanceMetricsArgsForCall(0)
					Expect(start).To(Equal(int64(0)))
				})

			})

			Context("when there is no end time in query string", func() {
				BeforeEach(func() {
					req, err = http.NewRequest(http.MethodGet, testUrlMemoryMetricHistories+"?start=123", nil)
					Expect(err).ToNot(HaveOccurred())
				})

				It("queries metrics from database with end time -1 ", func() {
					_, _, _, end := database.RetrieveInstanceMetricsArgsForCall(0)
					Expect(end).To(Equal(int64(-1)))
				})

			})

			Context("when query database succeeds", func() {
				BeforeEach(func() {
					req, err = http.NewRequest(http.MethodGet, testUrlMemoryMetricHistories+"?start=123&end=567", nil)
					Expect(err).ToNot(HaveOccurred())

					metric1 = models.AppInstanceMetric{
						AppId:         "an-app-id",
						InstanceIndex: 0,
						CollectedAt:   111122,
						Name:          models.MetricNameMemory,
						Unit:          models.UnitMegaBytes,
						Value:         "12345678",
						Timestamp:     111100,
					}

					metric2 = models.AppInstanceMetric{
						AppId:         "an-app-id",
						InstanceIndex: 1,
						CollectedAt:   111122,
						Name:          models.MetricNameMemory,
						Unit:          models.UnitMegaBytes,
						Value:         "87654321",
						Timestamp:     111111,
					}
					database.RetrieveInstanceMetricsReturns([]*models.AppInstanceMetric{&metric1, &metric2}, nil)
				})

				It("returns 200 with metrics in message body", func() {
					Expect(resp.Code).To(Equal(http.StatusOK))

					mtrcs := &[]models.AppInstanceMetric{}
					err = json.Unmarshal(resp.Body.Bytes(), mtrcs)

					Expect(err).ToNot(HaveOccurred())
					Expect(*mtrcs).To(Equal([]models.AppInstanceMetric{metric1, metric2}))
				})
			})

			Context("when query database fails", func() {
				BeforeEach(func() {
					req, err = http.NewRequest(http.MethodGet, testUrlMemoryMetricHistories+"?start=123&end=567", nil)
					Expect(err).ToNot(HaveOccurred())

					database.RetrieveInstanceMetricsReturns(nil, errors.New("database error"))
				})

				It("returns 500", func() {
					Expect(resp.Code).To(Equal(http.StatusInternalServerError))

					errJson := &models.ErrorResponse{}
					err = json.Unmarshal(resp.Body.Bytes(), errJson)

					Expect(err).ToNot(HaveOccurred())
					Expect(errJson).To(Equal(&models.ErrorResponse{
						Code:    "Interal-Server-Error",
						Message: "Error getting memory metric histories from database",
					}))
				})
			})

		})
	})
})
