package http_test

import (
	"testing"

	internalHttp "github.com/OmniTrustILM/cbom-repository/internal/http"
	"github.com/OmniTrustILM/cbom-repository/internal/service"
	"github.com/OmniTrustILM/cbom-repository/internal/store"

	mockS3 "github.com/OmniTrustILM/cbom-repository/internal/store/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestValidateDefaultBOMVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	s3Mock := mockS3.NewMockS3Contract(ctrl)
	s3Manager := mockS3.NewMockS3Manager(ctrl)

	svc, err := service.New(
		store.New(store.Config{Bucket: "something"}, s3Mock, s3Manager),
		service.Config{CheckOnFetch: false},
	)
	require.NoError(t, err)

	require.NoError(t, internalHttp.ValidateDefaultBOMVersion(internalHttp.Config{DefaultBOMVersion: "1.7"}, svc))
	require.Error(t, internalHttp.ValidateDefaultBOMVersion(internalHttp.Config{DefaultBOMVersion: "9.9"}, svc))
}
