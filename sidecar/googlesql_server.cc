// googlesql_server.cc — Standalone gRPC server for GoogleSQL local_service.
//
// This is a thin wrapper around GoogleSqlLocalServiceGrpcImpl that exposes
// the full GoogleSQL analyzer (Parse, Analyze, RegisterCatalog, etc.) over
// a TCP gRPC endpoint. LocalBQ starts this as a sidecar process and talks
// to it via the generated gRPC client.
//
// Build: bazel build -c opt //sidecar:googlesql_server
// Run:   ./googlesql_server --port=50051
//
// The Java bindings use the same gRPC service via JNI + in-process socketpair.
// We use a real TCP server because Go doesn't support JNI.

#include <csignal>
#include <iostream>
#include <memory>
#include <string>

#include "absl/flags/flag.h"
#include "absl/flags/parse.h"
#include "absl/strings/str_cat.h"
#include "grpcpp/grpcpp.h"
#include "grpcpp/health_check_service_interface.h"
#include "googlesql/local_service/local_service_grpc.h"

ABSL_FLAG(int32_t, port, 0,
          "Port to listen on. 0 means pick a random available port.");

namespace {

std::unique_ptr<grpc::Server> g_server;

void SignalHandler(int signal) {
  if (g_server) {
    g_server->Shutdown();
  }
}

}  // namespace

int main(int argc, char** argv) {
  absl::ParseCommandLine(argc, argv);

  int port = absl::GetFlag(FLAGS_port);
  std::string server_address = absl::StrCat("0.0.0.0:", port);

  grpc::EnableDefaultHealthCheckService(true);

  googlesql::local_service::GoogleSqlLocalServiceGrpcImpl service;
  grpc::ServerBuilder builder;

  int selected_port = 0;
  builder.AddListeningPort(server_address, grpc::InsecureServerCredentials(),
                           &selected_port);
  builder.RegisterService(&service);

  g_server = builder.BuildAndStart();
  if (!g_server) {
    std::cerr << "ERROR: Failed to start server on " << server_address
              << std::endl;
    return 1;
  }

  // Print the actual port to stdout so the parent process can read it.
  // This is the IPC mechanism: parent reads this line to know where to connect.
  std::cout << "GOOGLESQL_PORT=" << selected_port << std::endl;
  std::cout.flush();

  std::signal(SIGTERM, SignalHandler);
  std::signal(SIGINT, SignalHandler);

  g_server->Wait();
  return 0;
}
