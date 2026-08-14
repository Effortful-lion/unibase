package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewMinIO_EmptyEndpoint(t *testing.T) {
	_, err := NewMinIO(MinIOConfig{
		Endpoint:  "",
		AccessKey: "test",
		SecretKey: "test",
		Bucket:    "test",
	})
	assert.Error(t, err)
}

func TestMinIO_NotInitialized(t *testing.T) {
	m := &MinIO{}

	_, err := m.UploadObject(nil, "test.txt", "text/plain", []byte("hello"))
	assert.Error(t, err)

	_, err = m.DownloadObject(nil, "test.txt")
	assert.Error(t, err)

	err = m.DeleteObject(nil, "test.txt")
	assert.Error(t, err)

	_, err = m.PresignGet(nil, "test.txt", 0)
	assert.Error(t, err)

	_, err = m.PresignPut(nil, "test.txt", "", 0)
	assert.Error(t, err)

	_, err = m.StatObject(nil, "test.txt")
	assert.Error(t, err)

	_, err = m.ListObjects(nil, "")
	assert.Error(t, err)
}
