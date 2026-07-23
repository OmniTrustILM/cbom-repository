package service_test

import (
	"context"
	"io"
	"strings"
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

func TestNewFunc(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	s3Mock := mockS3.NewMockS3Contract(ctrl)
	s3Manager := mockS3.NewMockS3Manager(ctrl)

	svc, err := service.New(
		store.New(store.Config{Bucket: "something"}, s3Mock, s3Manager),
		service.Config{CheckOnFetch: false},
	)
	require.NoError(t, err)
	require.True(t, svc.VersionSupported("1.6"))
	require.True(t, svc.VersionSupported("1.7"))
	require.False(t, svc.VersionSupported("1.4"))
	require.Equal(t, []string{"1.6", "1.7"}, svc.SupportedVersion())
}

func TestSearch_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	s3Mock := mockS3.NewMockS3Contract(ctrl)
	// Return a single page with two objects where LastModified is recent
	now := time.Now()
	s3Mock.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.ListObjectsV2Output{
		Contents: []types.Object{
			{Key: awsString("urn:uuid:1-1"), LastModified: &now},
			{Key: awsString("urn:uuid:2-2"), LastModified: &now},
		},
	}, nil)
	s3Mock.EXPECT().HeadObject(gomock.Any(), &s3.HeadObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("urn:uuid:1-1"),
	}).Return(&s3.HeadObjectOutput{
		ContentLength: aws.Int64(123456),
		ContentType:   aws.String("application/vnd.cyclonedx+json"),
		LastModified:  &now,
		Metadata: map[string]string{
			store.MetaCryptoStatsKey: "{}",
		},
	}, nil)
	s3Mock.EXPECT().HeadObject(gomock.Any(), &s3.HeadObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("urn:uuid:2-2"),
	}).Return(&s3.HeadObjectOutput{
		ContentLength: aws.Int64(123456),
		ContentType:   aws.String("application/vnd.cyclonedx+json"),
		LastModified:  &now,
		Metadata: map[string]string{
			store.MetaCryptoStatsKey: "{}",
		},
	}, nil)

	st := store.New(store.Config{Bucket: "bucket"}, s3Mock, nil)
	svc, err := service.New(st, service.Config{CheckOnFetch: false})
	require.NoError(t, err)

	res, err := svc.Search(context.Background(), now.Unix()-1)
	require.NoError(t, err)
	require.Len(t, res, 2)
	require.Equal(t, "urn:uuid:1", res[0].SerialNumber)
	require.Equal(t, "1", res[0].Version)
}

func TestSearch_BadKey(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	s3Mock := mockS3.NewMockS3Contract(ctrl)
	now := time.Now()
	s3Mock.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.ListObjectsV2Output{
		Contents: []types.Object{{Key: awsString("badkey"), LastModified: &now}},
	}, nil)

	st := store.New(store.Config{Bucket: "bucket"}, s3Mock, nil)
	svc, err := service.New(st, service.Config{CheckOnFetch: false})
	require.NoError(t, err)

	_, err = svc.Search(context.Background(), now.Unix()-1)
	require.Error(t, err)
}

func TestGetBOMByUrn_VersionsNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	s3Mock := mockS3.NewMockS3Contract(ctrl)
	// ListObjectsV2 returns no contents -> store.GetObjectVersions returns ErrNotFound
	s3Mock.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.ListObjectsV2Output{Contents: []types.Object{}}, nil)

	st := store.New(store.Config{Bucket: "bucket"}, s3Mock, nil)
	svc, err := service.New(st, service.Config{CheckOnFetch: false})
	require.NoError(t, err)

	_, err = svc.GetBOMByUrn(context.Background(), "urn:uuid:123", "")
	require.ErrorIs(t, err, service.ErrNotFound)
}

func TestGetBOMByUrn_GetObjectNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	s3Mock := mockS3.NewMockS3Contract(ctrl)
	// ListObjectsV2 returns one object version
	now := time.Now()
	s3Mock.EXPECT().ListObjectsV2(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.ListObjectsV2Output{
		Contents: []types.Object{{Key: awsString("urn:uuid:123-1"), LastModified: &now}},
	}, nil)
	// GetObject returns NoSuchKey error
	s3Mock.EXPECT().GetObject(gomock.Any(), gomock.Any(), gomock.Any()).Return((*s3.GetObjectOutput)(nil), &types.NoSuchKey{})

	st := store.New(store.Config{Bucket: "bucket"}, s3Mock, nil)
	svc, err := service.New(st, service.Config{CheckOnFetch: false})
	require.NoError(t, err)

	_, err = svc.GetBOMByUrn(context.Background(), "urn:uuid:123", "")
	require.ErrorIs(t, err, service.ErrNotFound)
}

func TestGetBOMByUrn_UnmarshalError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	s3Mock := mockS3.NewMockS3Contract(ctrl)
	// When version is provided, service should call GetObject directly; return invalid JSON
	s3Mock.EXPECT().GetObject(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("not json"))}, nil)

	st := store.New(store.Config{Bucket: "bucket"}, s3Mock, nil)
	svc, err := service.New(st, service.Config{CheckOnFetch: true})
	require.NoError(t, err)

	_, err = svc.GetBOMByUrn(context.Background(), "urn:uuid:123", "1")
	require.Error(t, err)
}

func TestGetBOMByUrn_UnmarshalError_ButCheckOnFetchFalse(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	s3Mock := mockS3.NewMockS3Contract(ctrl)
	// When version is provided, service should call GetObject directly; return invalid JSON
	s3Mock.EXPECT().GetObject(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("not json"))}, nil)

	st := store.New(store.Config{Bucket: "bucket"}, s3Mock, nil)
	svc, err := service.New(st, service.Config{CheckOnFetch: false})
	require.NoError(t, err)

	_, err = svc.GetBOMByUrn(context.Background(), "urn:uuid:123", "1")
	require.NoError(t, err)
}

func TestGetBOMByUrn_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	s3Mock := mockS3.NewMockS3Contract(ctrl)
	// GetObject returns valid JSON
	s3Mock.EXPECT().GetObject(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("{\"bomFormat\":\"CycloneDX\"}"))}, nil)

	st := store.New(store.Config{Bucket: "bucket"}, s3Mock, nil)
	svc, err := service.New(st, service.Config{CheckOnFetch: false})
	require.NoError(t, err)

	res, err := svc.GetBOMByUrn(context.Background(), "urn:uuid:123", "1")
	require.NoError(t, err)
	require.IsType(t, []byte{}, res)
}

func TestGetBOMByUrn_Success_CheckOnFetchTrue(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	s3Mock := mockS3.NewMockS3Contract(ctrl)
	// GetObject returns valid JSON
	s3Mock.EXPECT().GetObject(gomock.Any(), gomock.Any(), gomock.Any()).Return(&s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("{\"bomFormat\":\"CycloneDX\"}"))}, nil)

	st := store.New(store.Config{Bucket: "bucket"}, s3Mock, nil)
	svc, err := service.New(st, service.Config{CheckOnFetch: true})
	require.NoError(t, err)

	res, err := svc.GetBOMByUrn(context.Background(), "urn:uuid:123", "1")
	require.NoError(t, err)
	require.IsType(t, []byte{}, res)
}

// helper to create *string for aws types
func awsString(s string) *string { return &s }
