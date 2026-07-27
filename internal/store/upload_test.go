package store_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/OmniTrustILM/cbom-repository/internal/store"
	mockS3 "github.com/OmniTrustILM/cbom-repository/internal/store/mock"

	cdx "github.com/CycloneDX/cyclonedx-go"
	manager "github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestStoreUpload(t *testing.T) {
	bucketName := "bucket"

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	okCBOM := cdx.BOM{
		BOMFormat:    cdx.BOMFormat,
		SpecVersion:  cdx.SpecVersion1_6,
		SerialNumber: "urn:uuid:5bd5a7c5-f5f0-40db-a216-d242abba1185",
		Version:      1,
		Metadata: &cdx.Metadata{
			Timestamp: time.Now().String(),
			Authors:   &[]cdx.OrganizationalContact{{Name: "John Doe", Email: "john@doe.com"}},
		},
	}

	tests := map[string]struct {
		key     string
		setup   func(mockCtrl *gomock.Controller, key string) store.Store
		wantErr bool
	}{
		"success": {
			key: "urn:uuid:5bd5a7c5-f5f0-40db-a216-d242abba1185-1",
			setup: func(mockCtrl *gomock.Controller, key string) store.Store {
				s3Mock := mockS3.NewMockS3Contract(mockCtrl)
				s3Manager := mockS3.NewMockS3Manager(mockCtrl)

				s3Manager.EXPECT().UploadObject(
					gomock.Any(),
					gomock.AssignableToTypeOf(&manager.UploadObjectInput{}),
				).DoAndReturn(func(_ context.Context, in *manager.UploadObjectInput, _ ...func(*manager.Options)) (*manager.UploadObjectOutput, error) {
					require.Equal(t, bucketName, *in.Bucket)
					require.Equal(t, key, *in.Key)
					return &manager.UploadObjectOutput{}, nil
				})

				return store.New(store.Config{Bucket: bucketName}, s3Mock, s3Manager)
			},
			wantErr: false,
		},
		"put object returns error": {
			key: "urn:uuid:5bd5a7c5-f5f0-40db-a216-d242abba1185-5",
			setup: func(mockCtrl *gomock.Controller, key string) store.Store {
				s3Mock := mockS3.NewMockS3Contract(mockCtrl)
				s3Manager := mockS3.NewMockS3Manager(mockCtrl)

				s3Manager.EXPECT().UploadObject(
					gomock.Any(),
					gomock.AssignableToTypeOf(&manager.UploadObjectInput{}),
				).DoAndReturn(func(_ context.Context, in *manager.UploadObjectInput, _ ...func(*manager.Options)) (*manager.UploadObjectOutput, error) {
					require.Equal(t, bucketName, *in.Bucket)
					require.Equal(t, key, *in.Key)
					return &manager.UploadObjectOutput{}, errors.New("abc")
				})

				return store.New(store.Config{Bucket: bucketName}, s3Mock, s3Manager)
			},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			s := tc.setup(ctrl, tc.key)
			var buf bytes.Buffer
			require.NoError(t, cdx.NewBOMEncoder(&buf, cdx.BOMFileFormatJSON).Encode(&okCBOM))

			meta := store.Metadata{
				Version:     "1",
				CryptoStats: "{}",
			}
			err := s.Upload(context.Background(), tc.key, meta, []byte("some bytes"))
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}

}
