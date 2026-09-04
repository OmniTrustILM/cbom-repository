package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/OmniTrustILM/cbom-repository/internal/service"
	"github.com/OmniTrustILM/cbom-repository/internal/store"
	mockS3 "github.com/OmniTrustILM/cbom-repository/internal/store/mock"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestService_UrnVersions(t *testing.T) {
	testTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	validUrn := "urn:uuid:3e671687-395b-41f5-a30f-a58921a69b79"

	cryptoStats := service.CryptoStats{
		CryptoAsset: service.CryptoAssetStats{
			Total: 10,
			Algo:  service.TotalStats{Total: 3},
			Cert:  service.TotalStats{Total: 2},
		},
	}
	cryptoStatsJSON, _ := json.Marshal(cryptoStats)

	tests := []struct {
		name      string
		urn       string
		setupMock func(*mockS3.MockS3Contract)
		want      []service.VersionRes
		wantErr   error
	}{
		{
			name: "success with multiple versions and original",
			urn:  validUrn,
			setupMock: func(m *mockS3.MockS3Contract) {
				m.EXPECT().
					ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&s3.ListObjectsV2Output{
						Contents: []types.Object{
							{Key: aws.String(validUrn + "-1")},
							{Key: aws.String(validUrn + "-2")},
							{Key: aws.String(validUrn + "-original")},
						},
					}, nil)

				// Return versions [1, 2] and hasOriginal = true
				m.EXPECT().
					HeadObject(gomock.Any(), gomock.Any()).
					Return(&s3.HeadObjectOutput{
						ContentLength: aws.Int64(1024),
						ContentType:   aws.String("application/json"),
						LastModified:  &testTime,
						Metadata: map[string]string{
							store.MetaCryptoStatsKey: string(cryptoStatsJSON),
						},
					}, nil).Times(3)
			},
			want: []service.VersionRes{
				{
					Version:     "1",
					Timestamp:   testTime.Format(time.RFC3339),
					CryptoStats: cryptoStats,
				},
				{
					Version:     "2",
					Timestamp:   testTime.Format(time.RFC3339),
					CryptoStats: cryptoStats,
				},
				{
					Version:     "original",
					Timestamp:   testTime.Format(time.RFC3339),
					CryptoStats: cryptoStats,
				},
			},
			wantErr: nil,
		},
		{
			name: "success with only numbered versions",
			urn:  validUrn,
			setupMock: func(m *mockS3.MockS3Contract) {
				m.EXPECT().
					ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&s3.ListObjectsV2Output{
						Contents: []types.Object{
							{Key: aws.String(validUrn + "-1")},
							{Key: aws.String(validUrn + "-2")},
						},
					}, nil)

				m.EXPECT().
					HeadObject(gomock.Any(), gomock.Any()).
					Return(&s3.HeadObjectOutput{
						ContentLength: aws.Int64(1024),
						ContentType:   aws.String("application/json"),
						LastModified:  &testTime,
						Metadata: map[string]string{
							store.MetaCryptoStatsKey: string(cryptoStatsJSON),
						},
					}, nil).Times(2)
			},
			want: []service.VersionRes{
				{
					Version:     "1",
					Timestamp:   testTime.Format(time.RFC3339),
					CryptoStats: cryptoStats,
				},
				{
					Version:     "2",
					Timestamp:   testTime.Format(time.RFC3339),
					CryptoStats: cryptoStats,
				},
			},
			wantErr: nil,
		},
		{
			name: "GetObjectVersions returns ErrNotFound",
			urn:  validUrn,
			setupMock: func(m *mockS3.MockS3Contract) {
				m.EXPECT().
					ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, store.ErrNotFound)
			},
			want:    nil,
			wantErr: errors.New("obtaining next page failed"),
		},
		{
			name: "GetObjectVersions returns other error",
			urn:  validUrn,
			setupMock: func(m *mockS3.MockS3Contract) {
				m.EXPECT().
					ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, errors.New("s3 error"))
			},
			want:    nil,
			wantErr: errors.New("obtaining next page failed"),
		},
		{
			name: "HeadObject returns ErrNotFound - skip version",
			urn:  validUrn,
			setupMock: func(m *mockS3.MockS3Contract) {
				m.EXPECT().
					ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&s3.ListObjectsV2Output{
						Contents: []types.Object{
							{Key: aws.String(validUrn + "-1")},
							{Key: aws.String(validUrn + "-2")},
						},
					}, nil)

				gomock.InOrder(
					m.EXPECT().
						HeadObject(gomock.Any(), gomock.Any()).
						Return(nil, &types.NoSuchKey{Message: ptr("no such key")}),
					m.EXPECT().
						HeadObject(gomock.Any(), gomock.Any()).
						Return(&s3.HeadObjectOutput{
							ContentLength: aws.Int64(1024),
							ContentType:   aws.String("application/json"),
							LastModified:  &testTime,
							Metadata: map[string]string{
								store.MetaCryptoStatsKey: string(cryptoStatsJSON),
							},
						}, nil),
				)
			},
			want: []service.VersionRes{
				{
					Version:     "2",
					Timestamp:   testTime.Format(time.RFC3339),
					CryptoStats: cryptoStats,
				},
			},
			wantErr: nil,
		},
		{
			name: "HeadObject returns other error",
			urn:  validUrn,
			setupMock: func(m *mockS3.MockS3Contract) {
				m.EXPECT().
					ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&s3.ListObjectsV2Output{
						Contents: []types.Object{
							{Key: aws.String(validUrn + "-1")},
							{Key: aws.String(validUrn + "-2")},
						},
					}, nil)

				m.EXPECT().
					HeadObject(gomock.Any(), gomock.Any()).
					Return(nil, errors.New("head object error"))
			},
			want:    nil,
			wantErr: errors.New("`s3.HeadObject()` failed"),
		},
		{
			name: "missing crypto stats metadata - skip version",
			urn:  validUrn,
			setupMock: func(m *mockS3.MockS3Contract) {
				m.EXPECT().
					ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&s3.ListObjectsV2Output{
						Contents: []types.Object{
							{Key: aws.String(validUrn + "-1")},
							{Key: aws.String(validUrn + "-2")},
						},
					}, nil)

				gomock.InOrder(
					m.EXPECT().
						HeadObject(gomock.Any(), gomock.Any()).
						Return(&s3.HeadObjectOutput{
							ContentLength: aws.Int64(1024),
							ContentType:   aws.String("application/json"),
							LastModified:  &testTime,
							Metadata:      map[string]string{},
						}, nil),

					m.EXPECT().
						HeadObject(gomock.Any(), gomock.Any()).
						Return(&s3.HeadObjectOutput{
							ContentLength: aws.Int64(1024),
							ContentType:   aws.String("application/json"),
							LastModified:  &testTime,
							Metadata: map[string]string{
								store.MetaCryptoStatsKey: string(cryptoStatsJSON),
							},
						}, nil),
				)
			},
			want: []service.VersionRes{
				{
					Version:     "2",
					Timestamp:   testTime.Format(time.RFC3339),
					CryptoStats: cryptoStats,
				},
			},
			wantErr: nil,
		},
		{
			name: "invalid crypto stats json",
			urn:  validUrn,
			setupMock: func(m *mockS3.MockS3Contract) {
				m.EXPECT().
					ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&s3.ListObjectsV2Output{
						Contents: []types.Object{
							{Key: aws.String(validUrn + "-1")},
						},
					}, nil)

				m.EXPECT().
					HeadObject(gomock.Any(), gomock.Any()).
					Return(&s3.HeadObjectOutput{
						ContentLength: aws.Int64(1024),
						ContentType:   aws.String("application/json"),
						LastModified:  &testTime,
						Metadata: map[string]string{
							store.MetaCryptoStatsKey: "invalid json",
						},
					}, nil)
			},
			want:    []service.VersionRes{},
			wantErr: errors.New("unmarshaling json failed"),
		},
		{
			name: "empty versions list",
			urn:  validUrn,
			setupMock: func(m *mockS3.MockS3Contract) {
				m.EXPECT().
					ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&s3.ListObjectsV2Output{}, nil)
			},
			want:    []service.VersionRes{},
			wantErr: service.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			s3Mock := mockS3.NewMockS3Contract(ctrl)
			tt.setupMock(s3Mock)

			st := store.New(store.Config{Bucket: "bucket"}, s3Mock, nil)
			svc, err := service.New(st, service.Config{CheckOnFetch: false})
			require.NoError(t, err)

			got, err := svc.UrnVersions(context.Background(), tt.urn)

			if tt.wantErr != nil {
				require.Error(t, err)

				if errors.Is(tt.wantErr, service.ErrNotFound) {
					require.ErrorIs(t, err, service.ErrNotFound)
				} else {
					require.Contains(t, err.Error(), tt.wantErr.Error())
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func ptr[T any](v T) *T {
	return &v
}
