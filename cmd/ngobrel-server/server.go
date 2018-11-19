package main

import (
	"fmt"
	"log"
	"net"
	"os"

	minio "github.com/minio/minio-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	pb "ngobrel.rocks/ngobrel"
)

func main() {
	fmt.Println("OK o!")

	port := ":8000"
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	var smsClient pb.Sms
	smsAccount, smsAccountExists := os.LookupEnv("SMS_ACCOUNT")

	if smsAccountExists {
		smsClient = pb.NewTwilioSms()
	} else {
		smsClient = pb.NewDummySms()
	}
	smsClient.SetAccount(smsAccount, os.Getenv("SMS_TOKEN"))

	minioClient, err := minio.New(os.Getenv("MINIO_URL"), os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"), false)
	if err != nil {
		log.Println("Minio error: " + os.Getenv("MINIO_URL"))
		log.Fatalln(err)
	}

	pb.InitDB()
	s := grpc.NewServer()
	server := pb.NewServer(smsClient, *minioClient)
	pb.RegisterNgobrelServer(s, server)
	// Register reflection service on gRPC server.
	reflection.Register(s)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
