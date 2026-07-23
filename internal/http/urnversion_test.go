package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/OmniTrustILM/cbom-repository/internal/health"
	"github.com/OmniTrustILM/cbom-repository/internal/service"
	"github.com/OmniTrustILM/cbom-repository/internal/store"
	mockS3 "github.com/OmniTrustILM/cbom-repository/internal/store/mock"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestServer_URNVersions(t *testing.T) {
	cryptoStats := service.CryptoStats{
		CryptoAsset: service.CryptoAssetStats{
			Total: 10,
			Algo:  service.TotalStats{Total: 3},
			Cert:  service.TotalStats{Total: 2},
		},
	}
	cryptoStatsJSON, _ := json.Marshal(cryptoStats)

	tests := []struct {
		name           string
		urn            string
		prefix         string
		setupMock      func(*mockS3.MockS3Contract)
		expectedStatus int
		expectedBody   map[string]any
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:   "successful retrieval with default prefix",
			urn:    "urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79",
			prefix: "/api",
			setupMock: func(mock *mockS3.MockS3Contract) {
				mock.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.ListObjectsV2Output{
					Contents: []types.Object{
						{Key: aws.String("urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79-1")},
						{Key: aws.String("urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79-2")},
						{Key: aws.String("urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79-3")},
					},
				}, nil)
				mock.EXPECT().
					HeadObject(gomock.Any(), gomock.Any()).
					Return(&s3.HeadObjectOutput{
						ContentLength: aws.Int64(1024),
						ContentType:   aws.String("application/json"),
						LastModified:  aws.Time(time.Now()),
						Metadata: map[string]string{
							store.MetaCryptoStatsKey: string(cryptoStatsJSON),
						},
					}, nil).Times(3)
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
				response := []service.VersionRes{}
				err := json.NewDecoder(rec.Body).Decode(&response)
				require.NoError(t, err)
				require.Equal(t, "1", response[0].Version)
				require.Equal(t, "2", response[1].Version)
				require.Equal(t, "3", response[2].Version)
			},
		},
		{
			name:   "successful retrieval with original version and custom prefix",
			urn:    "urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79",
			prefix: "/v1/api",
			setupMock: func(mock *mockS3.MockS3Contract) {
				mock.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.ListObjectsV2Output{
					Contents: []types.Object{
						{Key: aws.String("urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79-original")},
						{Key: aws.String("urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79-1")},
						{Key: aws.String("urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79-5")},
					},
				}, nil)
				mock.EXPECT().
					HeadObject(gomock.Any(), gomock.Any()).
					Return(&s3.HeadObjectOutput{
						ContentLength: aws.Int64(1024),
						ContentType:   aws.String("application/json"),
						LastModified:  aws.Time(time.Now()),
						Metadata: map[string]string{
							store.MetaCryptoStatsKey: string(cryptoStatsJSON),
						},
					}, nil).Times(3)
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
				response := []service.VersionRes{}
				err := json.NewDecoder(rec.Body).Decode(&response)
				require.NoError(t, err)
				require.Equal(t, 3, len(response))
			},
		},
		{
			name:   "empty prefix",
			urn:    "urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79",
			prefix: "",
			setupMock: func(mock *mockS3.MockS3Contract) {
				mock.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.ListObjectsV2Output{
					Contents: []types.Object{
						{Key: aws.String("urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79-1")},
					},
				}, nil)
				mock.EXPECT().
					HeadObject(gomock.Any(), gomock.Any()).
					Return(&s3.HeadObjectOutput{
						ContentLength: aws.Int64(1024),
						ContentType:   aws.String("application/json"),
						LastModified:  aws.Time(time.Now()),
						Metadata: map[string]string{
							store.MetaCryptoStatsKey: string(cryptoStatsJSON),
						},
					}, nil)
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
				response := []service.VersionRes{}
				err := json.NewDecoder(rec.Body).Decode(&response)
				require.NoError(t, err)
				require.Equal(t, 1, len(response))
			},
		},
		{
			name:           "invalid URN - not enough parts",
			urn:            "invalid:urn",
			prefix:         "/api",
			setupMock:      func(mock *mockS3.MockS3Contract) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
			},
		},
		{
			name:           "invalid URN - wrong namespace",
			urn:            "urn:isbn:123456789",
			prefix:         "/api",
			setupMock:      func(mock *mockS3.MockS3Contract) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
			},
		},
		{
			name:           "invalid URN - not a UUID",
			urn:            "urn:uuid:not-a-uuid",
			prefix:         "/api",
			setupMock:      func(mock *mockS3.MockS3Contract) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
			},
		},
		{
			name:   "not found - no versions exist",
			urn:    "urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79",
			prefix: "/api",
			setupMock: func(mock *mockS3.MockS3Contract) {
				mock.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.ListObjectsV2Output{
					Contents: []types.Object{},
				}, nil)
			},
			expectedStatus: http.StatusNotFound,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
			},
		},
		{
			name:   "internal error - store failure",
			urn:    "urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79",
			prefix: "/api",
			setupMock: func(mock *mockS3.MockS3Contract) {
				mock.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(
					nil, errors.New("s3 connection failed"),
				)
			},
			expectedStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
			},
		},
		{
			name:   "single version without original",
			urn:    "urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79",
			prefix: "/custom/path",
			setupMock: func(mock *mockS3.MockS3Contract) {
				mock.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.ListObjectsV2Output{
					Contents: []types.Object{
						{Key: aws.String("urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79-1")},
					},
				}, nil)
				mock.EXPECT().
					HeadObject(gomock.Any(), gomock.Any()).
					Return(&s3.HeadObjectOutput{
						ContentLength: aws.Int64(1024),
						ContentType:   aws.String("application/json"),
						LastModified:  aws.Time(time.Now()),
						Metadata: map[string]string{
							store.MetaCryptoStatsKey: string(cryptoStatsJSON),
						},
					}, nil)
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
				response := []service.VersionRes{}
				err := json.NewDecoder(rec.Body).Decode(&response)
				require.NoError(t, err)
				require.Equal(t, 1, len(response))
			},
		},
		{
			name:   "multiple versions in non-sequential order",
			urn:    "urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79",
			prefix: "/api",
			setupMock: func(mock *mockS3.MockS3Contract) {
				mock.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.ListObjectsV2Output{
					Contents: []types.Object{
						{Key: aws.String("urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79-5")},
						{Key: aws.String("urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79-1")},
						{Key: aws.String("urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79-10")},
						{Key: aws.String("urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79-3")},
					},
				}, nil)
				mock.EXPECT().
					HeadObject(gomock.Any(), gomock.Any()).
					Return(&s3.HeadObjectOutput{
						ContentLength: aws.Int64(1024),
						ContentType:   aws.String("application/json"),
						LastModified:  aws.Time(time.Now()),
						Metadata: map[string]string{
							store.MetaCryptoStatsKey: string(cryptoStatsJSON),
						},
					}, nil).Times(4)
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rec *httptest.ResponseRecorder) {
				require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
				response := []service.VersionRes{}
				err := json.NewDecoder(rec.Body).Decode(&response)
				require.NoError(t, err)
				require.Equal(t, 4, len(response))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			s3Mock := mockS3.NewMockS3Contract(ctrl)
			tt.setupMock(s3Mock)

			st := store.New(store.Config{Bucket: "test-bucket"}, s3Mock, nil)
			svc, err := service.New(st, service.Config{CheckOnFetch: false})
			require.NoError(t, err)

			storageChecker := mockChecker{name: "storage", status: health.StatusUp, details: map[string]any{"latencyMs": 1}}
			healthSvc := health.NewService(storageChecker)

			cfg := Config{
				Port:   8080,
				Prefix: tt.prefix,
			}
			server := New(cfg, svc, healthSvc)

			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("%s%s", tt.prefix, RouteBOMVersions), nil)
			req = mux.SetURLVars(req, map[string]string{"urn": tt.urn})
			rec := httptest.NewRecorder()

			server.URNVersions(rec, req)

			require.Equal(t, tt.expectedStatus, rec.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, rec)
			}
		})
	}
}
