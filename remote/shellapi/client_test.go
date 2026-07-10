package shellapi_test

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/lib-x/lpk-go/remote/shellapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

type shellServer struct {
	shellapi.UnimplementedShellCoreServer
	t             *testing.T
	credential    string
	coreInfoCalls int
	modified      *shellapi.BoxSetting
	dialRequest   *shellapi.DialBoxServiceRequest
	mu            sync.Mutex
}

func (server *shellServer) checkCredential(ctx context.Context) {
	server.t.Helper()
	values := metadata.ValueFromIncomingContext(ctx, "lzc-shellapi-cred")
	if len(values) != 1 || values[0] != server.credential {
		server.t.Errorf("credential metadata = %#v", values)
	}
}

func (server *shellServer) QueryBoxList(ctx context.Context, _ *emptypb.Empty) (*shellapi.BoxList, error) {
	server.checkCredential(ctx)
	return &shellapi.BoxList{Boxes: []*shellapi.BoxInfo{
		{BoxId: "box-1", BoxName: "first", LoginUser: "user", Status: shellapi.BoxStatus_READY},
		{BoxId: "box-2", BoxName: "second", LoginUser: "admin", Status: shellapi.BoxStatus_READY, IsDefaultBox: true, IsAdminLogin: true},
	}}, nil
}

func (server *shellServer) QueryShellCoreInfo(ctx context.Context, _ *emptypb.Empty) (*shellapi.ShellCoreInfo, error) {
	server.checkCredential(ctx)
	server.mu.Lock()
	server.coreInfoCalls++
	server.mu.Unlock()
	return &shellapi.ShellCoreInfo{Id: "client-123", Version: "1.0.0", DeviceOs: "linux"}, nil
}

func (server *shellServer) ModifyBoxConfig(ctx context.Context, input *shellapi.BoxSetting) (*emptypb.Empty, error) {
	server.checkCredential(ctx)
	server.modified = input
	return &emptypb.Empty{}, nil
}

func (server *shellServer) DialBoxService(input *shellapi.DialBoxServiceRequest, stream grpc.ServerStreamingServer[shellapi.DialBoxServiceReply]) error {
	server.checkCredential(stream.Context())
	server.dialRequest = input
	if err := stream.Send(&shellapi.DialBoxServiceReply{LocalProxyAddress: "127.0.0.1:4567", ServiceExtraInfo: "ready"}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return nil
}

func TestClientUsesCredentialAndTypedShellAPI(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	server := &shellServer{t: t, credential: "shell-secret"}
	shellapi.RegisterShellCoreServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})
	client, err := shellapi.New(context.Background(), shellapi.Options{
		Config: shellapi.Config{Address: "passthrough:///bufnet", Credential: "shell-secret"},
		DialOptions: []grpc.DialOption{grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	box, err := client.DefaultBox(context.Background())
	if err != nil || box.ID != "box-2" || box.Name != "second" || !box.IsAdminLogin {
		t.Fatalf("box=%#v err=%v", box, err)
	}
	if err := client.SetDefaultBox(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if server.modified == nil || server.modified.GetId() != "box-1" || !server.modified.GetSetAsDefaultBox() {
		t.Fatalf("modified = %#v", server.modified)
	}
	firstID, err := client.ClientID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := client.ClientID(context.Background())
	if err != nil || firstID != "client-123" || secondID != firstID || server.coreInfoCalls != 1 {
		t.Fatalf("ids=%q/%q calls=%d err=%v", firstID, secondID, server.coreInfoCalls, err)
	}
	tunnel, err := client.DialBoxService(context.Background(), "box-2", "debug.bridge")
	if err != nil {
		t.Fatal(err)
	}
	if tunnel.Address != "127.0.0.1:4567" || tunnel.ExtraInfo != "ready" || server.dialRequest.GetServiceName() != "debug.bridge" {
		t.Fatalf("tunnel=%#v request=%#v", tunnel, server.dialRequest)
	}
	if err := tunnel.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestClientFallbackProvidesDefaultBoxWithoutGRPC(t *testing.T) {
	client, err := shellapi.New(context.Background(), shellapi.Options{Config: shellapi.Config{Fallback: true, UID: "user", BoxName: "box"}})
	if err != nil {
		t.Fatal(err)
	}
	box, err := client.DefaultBox(context.Background())
	if err != nil || box.Name != "box" || box.LoginUser != "user" || !box.IsDefault {
		t.Fatalf("box=%#v err=%v", box, err)
	}
}
