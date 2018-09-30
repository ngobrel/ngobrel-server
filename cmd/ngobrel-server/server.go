package main

import (
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	pb "ngobrel.rocks/ngobrel"
)

func main() {
	fmt.Println("OK!")

	port := ":8000"
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	pb.InitDB()
	s := grpc.NewServer()
	server := pb.NewServer()
	pb.RegisterNgobrelServer(s, server)
	// Register reflection service on gRPC server.
	reflection.Register(s)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
