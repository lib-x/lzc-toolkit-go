package shellapi

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

const defaultTimeout = 15 * time.Second

type Options struct {
	Config      Config
	Timeout     time.Duration
	DialOptions []grpc.DialOption
}

type Box struct {
	ID             string
	Name           string
	HomeURL        string
	Domain         string
	VirtualIP      string
	Status         BoxStatus
	StatusReason   string
	LoginUser      string
	IsAdminLogin   bool
	IsDefault      bool
	ConnectionType LowNetworkConnType
}

type CoreInfo struct {
	ID           string
	TunnelIP     string
	LocalIPs     []string
	OriginServer string
	DeviceDomain string
	Version      string
	DeviceOS     string
}

type Client struct {
	config  Config
	timeout time.Duration
	conn    *grpc.ClientConn
	rpc     ShellCoreClient

	clientIDMu sync.Mutex
	clientID   string
}

func New(ctx context.Context, options Options) (*Client, error) {
	if ctx == nil {
		return nil, shellError(lpkgo.CodeInvalidArgument, "shellapi.new", errors.New("nil context"))
	}
	config := options.Config
	config.Address = strings.TrimSpace(config.Address)
	config.Credential = strings.TrimSpace(config.Credential)
	config.UID = strings.TrimSpace(config.UID)
	config.BoxName = strings.TrimSpace(config.BoxName)
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	client := &Client{config: config, timeout: timeout}
	if config.Fallback {
		if config.UID == "" || config.BoxName == "" {
			return nil, shellError(lpkgo.CodeInvalidConfig, "shellapi.new", errors.New("fallback UID and box name are required"))
		}
		return client, nil
	}
	if config.Address == "" || config.Credential == "" {
		return nil, shellError(lpkgo.CodeInvalidConfig, "shellapi.new", errors.New("ShellAPI address and credential are required"))
	}
	dialOptions := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	dialOptions = append(dialOptions, options.DialOptions...)
	conn, err := grpc.NewClient(config.Address, dialOptions...)
	if err != nil {
		return nil, shellError(lpkgo.CodeRemoteUnavailable, "shellapi.new", errors.New("ShellAPI connection setup failed"))
	}
	client.conn = conn
	client.rpc = NewShellCoreClient(conn)
	return client, nil
}

func (client *Client) Close() error {
	if client == nil || client.conn == nil {
		return nil
	}
	return client.conn.Close()
}

func (client *Client) Boxes(ctx context.Context) ([]Box, error) {
	if err := client.validate(ctx, "shellapi.boxes"); err != nil {
		return nil, err
	}
	if client.rpc == nil {
		return []Box{{Name: client.config.BoxName, LoginUser: client.config.UID, Status: BoxStatus_READY, IsDefault: true}}, nil
	}
	callCtx, cancel := client.callContext(ctx)
	defer cancel()
	reply, err := client.rpc.QueryBoxList(callCtx, &emptypb.Empty{})
	if err != nil {
		return nil, shellError(lpkgo.CodeRemoteUnavailable, "shellapi.boxes", errors.New("ShellAPI box query failed"))
	}
	boxes := make([]Box, 0, len(reply.GetBoxes()))
	for _, item := range reply.GetBoxes() {
		if item == nil {
			continue
		}
		boxes = append(boxes, Box{
			ID: item.GetBoxId(), Name: item.GetBoxName(), HomeURL: item.GetBoxHomeUrl(), Domain: item.GetBoxDomain(),
			VirtualIP: item.GetBoxVirtualIp(), Status: item.GetStatus(), StatusReason: item.GetStatusReason(),
			LoginUser: item.GetLoginUser(), IsAdminLogin: item.GetIsAdminLogin(), IsDefault: item.GetIsDefaultBox(), ConnectionType: item.GetConnType(),
		})
	}
	return boxes, nil
}

func (client *Client) DefaultBox(ctx context.Context) (Box, error) {
	boxes, err := client.Boxes(ctx)
	if err != nil {
		return Box{}, err
	}
	for _, box := range boxes {
		if box.IsDefault {
			return box, nil
		}
	}
	return Box{}, shellError(lpkgo.CodeNotFound, "shellapi.default_box", errors.New("default box not found"))
}

func (client *Client) SetDefaultBox(ctx context.Context, boxName string) error {
	if err := client.validate(ctx, "shellapi.set_default_box"); err != nil {
		return err
	}
	if client.rpc == nil {
		return shellError(lpkgo.CodeIncompatibleBackend, "shellapi.set_default_box", errors.New("fallback mode cannot modify boxes"))
	}
	boxName = strings.TrimSpace(boxName)
	if boxName == "" {
		return shellError(lpkgo.CodeInvalidArgument, "shellapi.set_default_box", errors.New("box name is required"))
	}
	boxes, err := client.Boxes(ctx)
	if err != nil {
		return err
	}
	var selected *Box
	for index := range boxes {
		if boxes[index].Name == boxName {
			selected = &boxes[index]
			break
		}
	}
	if selected == nil {
		return shellError(lpkgo.CodeNotFound, "shellapi.set_default_box", errors.New("box not found"))
	}
	name := selected.Name
	value := true
	callCtx, cancel := client.callContext(ctx)
	defer cancel()
	_, err = client.rpc.ModifyBoxConfig(callCtx, &BoxSetting{Id: selected.ID, Name: &name, SetAsDefaultBox: &value})
	if err != nil {
		return shellError(lpkgo.CodeRemoteUnavailable, "shellapi.set_default_box", errors.New("ShellAPI box update failed"))
	}
	return nil
}

func (client *Client) ShellCoreInfo(ctx context.Context) (CoreInfo, error) {
	if err := client.validate(ctx, "shellapi.core_info"); err != nil {
		return CoreInfo{}, err
	}
	if client.rpc == nil {
		return CoreInfo{}, nil
	}
	callCtx, cancel := client.callContext(ctx)
	defer cancel()
	reply, err := client.rpc.QueryShellCoreInfo(callCtx, &emptypb.Empty{})
	if err != nil {
		return CoreInfo{}, shellError(lpkgo.CodeRemoteUnavailable, "shellapi.core_info", errors.New("ShellAPI core info query failed"))
	}
	return CoreInfo{
		ID: reply.GetId(), TunnelIP: reply.GetTunIp(), LocalIPs: append([]string(nil), reply.GetLocalIps()...),
		OriginServer: reply.GetOriginServer(), DeviceDomain: reply.GetDeviceDomain(), Version: reply.GetVersion(), DeviceOS: reply.GetDeviceOs(),
	}, nil
}

func (client *Client) ClientID(ctx context.Context) (string, error) {
	if err := client.validate(ctx, "shellapi.client_id"); err != nil {
		return "", err
	}
	client.clientIDMu.Lock()
	defer client.clientIDMu.Unlock()
	if client.clientID != "" {
		return client.clientID, nil
	}
	info, err := client.ShellCoreInfo(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(info.ID) == "" {
		return "", shellError(lpkgo.CodeNotFound, "shellapi.client_id", errors.New("ShellAPI client ID is empty"))
	}
	client.clientID = strings.TrimSpace(info.ID)
	return client.clientID, nil
}

type ServiceTunnel struct {
	Address   string
	ExtraInfo string
	cancel    context.CancelFunc
	stream    grpc.ServerStreamingClient[DialBoxServiceReply]
	once      sync.Once
}

func (tunnel *ServiceTunnel) Close() error {
	if tunnel == nil {
		return nil
	}
	tunnel.once.Do(func() {
		if tunnel.cancel != nil {
			tunnel.cancel()
		}
	})
	return nil
}

func (client *Client) DialBoxService(ctx context.Context, boxID, serviceName string) (*ServiceTunnel, error) {
	if err := client.validate(ctx, "shellapi.dial_box_service"); err != nil {
		return nil, err
	}
	if client.rpc == nil {
		return nil, shellError(lpkgo.CodeIncompatibleBackend, "shellapi.dial_box_service", errors.New("fallback mode cannot dial box services"))
	}
	boxID = strings.TrimSpace(boxID)
	serviceName = strings.TrimSpace(serviceName)
	if boxID == "" || serviceName == "" {
		return nil, shellError(lpkgo.CodeInvalidArgument, "shellapi.dial_box_service", errors.New("box ID and service name are required"))
	}
	streamCtx, cancel := context.WithCancel(client.metadataContext(ctx))
	stream, err := client.rpc.DialBoxService(streamCtx, &DialBoxServiceRequest{BoxId: boxID, ServiceName: serviceName})
	if err != nil {
		cancel()
		return nil, shellError(lpkgo.CodeRemoteUnavailable, "shellapi.dial_box_service", errors.New("ShellAPI service dial failed"))
	}
	for {
		reply, err := stream.Recv()
		if err != nil {
			cancel()
			if errors.Is(err, io.EOF) {
				return nil, shellError(lpkgo.CodeRemoteUnavailable, "shellapi.dial_box_service", errors.New("ShellAPI service stream ended before address"))
			}
			return nil, shellError(lpkgo.CodeRemoteUnavailable, "shellapi.dial_box_service", errors.New("ShellAPI service stream failed"))
		}
		if address := strings.TrimSpace(reply.GetLocalProxyAddress()); address != "" {
			return &ServiceTunnel{Address: address, ExtraInfo: reply.GetServiceExtraInfo(), cancel: cancel, stream: stream}, nil
		}
	}
}

func (client *Client) validate(ctx context.Context, op string) error {
	if ctx == nil || client == nil {
		return shellError(lpkgo.CodeInvalidArgument, op, errors.New("nil context or client"))
	}
	if err := ctx.Err(); err != nil {
		return shellError(lpkgo.CodeCancelled, op, err)
	}
	return nil
}

func (client *Client) metadataContext(ctx context.Context) context.Context {
	if client.config.Credential == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "lzc-shellapi-cred", client.config.Credential)
}

func (client *Client) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(client.metadataContext(ctx), client.timeout)
}

func shellError(code lpkgo.Code, op string, cause error) error {
	return &lpkgo.Error{Code: code, Op: op, Cause: cause}
}
