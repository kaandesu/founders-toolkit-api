package bucket

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Service struct {
	client *minio.Client
}

func New() *Service {
	var (
		endpoint        = os.Getenv("BUCKET_ENDPOINT")
		accessKeyID     = os.Getenv("BUCKET_ACCESS_KEY")
		secretAccessKey = os.Getenv("BUCKET_SECRET_KEY")
	)
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: true,
	})
	if err != nil {
		// TODO: don't fatal here
		log.Fatal(err)
	}

	return &Service{client: minioClient}
}

func (s *Service) ListBuckets() ([]minio.BucketInfo, error) {
	buckets, err := s.client.ListBuckets(context.Background())
	for _, bucket := range buckets {
		log.Println(bucket)
	}

	return buckets, err
}

func (s *Service) ListObjects() {
	opts := minio.ListObjectsOptions{
		UseV1:  true,
		Prefix: "",
	}

	// List all objects from a bucket-name with a matching prefix.
	for object := range s.client.ListObjects(context.Background(), "kova-1", opts) {
		if object.Err != nil {
			fmt.Println(object.Err)
			break
		}
		fmt.Println(object)
	}
}
