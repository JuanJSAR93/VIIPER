package cmd

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/Alia5/VIIPER/device/xboxone"
	"github.com/Alia5/VIIPER/internal/server/api/auth"
	"github.com/Alia5/VIIPER/viipertypes"
)

const (
	xboxOneBrokerMagic       = "X1BR"
	xboxOneBrokerVersion     = 1
	xboxOneConsumerReady     = 0x01
	xboxOneSemanticInput     = 0x02
	xboxOneCanonicalAck      = 0x03
	xboxOneConsumerAck       = 0x81
	xboxOneSemanticAck       = 0x82
	xboxOneCanonicalFeedback = 0x83
)

// XboxOneClient is the authenticated feeder for the retained Xbox One/Series
// persona. It is a subcommand of the main VIIPER executable; it does not
// replace the server-side retained registration or generic device factory.
type XboxOneClient struct {
	Addr                string `help:"VIIPER API address" default:"127.0.0.1:3242"`
	KeyFile             string `help:"VIIPER API password file" required:""`
	PauseBeforeActivate bool   `help:"leave the authorized persona ready for an external usbip attach test"`
	HoldSeconds         int    `help:"keep the attached persona alive after the input acknowledgement" default:"3"`
	InputTest           bool   `help:"send a live input matrix and verify each semantic ACK"`
	BusID               uint   `help:"use an existing VIIPER bus instead of creating a new one"`
	Profile             string `help:"Xbox identity profile" enum:"xboxone,xboxseries" default:"xboxone"`
}

type xboxOneRegistration struct {
	BusID      uint32
	DevID      string
	Removal    string
	USBIPBusID string
	RemoveMs   uint32
}

type xboxOneCreateResponse struct {
	BusID      uint32 `json:"busId"`
	DevID      string `json:"devId"`
	Removal    string `json:"removalToken"`
	USBIPBusID string `json:"usbipBusId"`
	RemoveMs   uint32 `json:"removalTimeoutMilliseconds"`
	USBIPPort  int    `json:"usbipPort"`
}

type xboxOneActivationResponse struct {
	Version    uint16 `json:"version"`
	USBIPBusID string `json:"usbipBusId"`
	USBIPPort  int    `json:"usbipPort"`
}

// Run executes the complete Xbox One/Series registration, activation, input
// and exact-removal flow. All errors are returned to Kong instead of exiting
// the hosting process, which keeps the main VIIPER binary usable as a server.
func (c *XboxOneClient) Run() error {
	if c == nil {
		return errors.New("Xbox One client configuration is nil")
	}
	if strings.TrimSpace(c.KeyFile) == "" {
		return errors.New("--key-file is required")
	}
	passwordBytes, err := os.ReadFile(c.KeyFile)
	if err != nil {
		return err
	}
	password := strings.TrimSpace(string(passwordBytes))
	key, err := auth.DeriveKey(password)
	if err != nil {
		return err
	}

	addr := strings.TrimSpace(c.Addr)
	if addr == "" {
		addr = "127.0.0.1:3242"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client := &xboxOneAPIClient{addr: addr, key: key}

	vendorID := uint16(0x045e)
	productID := uint16(0x02ea)
	product := "VIIPER Xbox One Controller"
	switch strings.ToLower(strings.TrimSpace(c.Profile)) {
	case "xboxone":
	case "xboxseries":
		productID = 0x0b12
		product = "VIIPER Xbox Series X|S Controller"
	default:
		return fmt.Errorf("perfil Xbox no soportado: %s", c.Profile)
	}

	var bus struct {
		BusID uint32 `json:"busId"`
	}
	if c.BusID != 0 {
		bus.BusID = uint32(c.BusID)
	} else {
		busRaw, err := client.request(ctx, "bus/create", "0")
		if err != nil {
			return fmt.Errorf("crear bus: %w", err)
		}
		if err := json.Unmarshal(busRaw, &bus); err != nil || bus.BusID == 0 {
			return fmt.Errorf("respuesta bus inválida: %s", busRaw)
		}
	}

	// The retained-USB registry binds the GIP DeviceID to the USB serial.
	// Generate a fresh lab identity for every run so a crashed test cannot
	// poison the next invocation with a stale primary registration.
	deviceID := uint64(0x0000fffb00000000) | uint64(time.Now().UnixNano()&0xffffffff)
	serial := fmt.Sprintf("%016xA1B2C3D4E5F60706", deviceID)
	create := viipertypes.XboxOneAuthorizedCreateRequestV1{
		Version:                      1,
		IdentityAuthorizationGranted: true,
		Identity: viipertypes.XboxOneAuthorizedIdentityV1{
			VendorID: vendorID, ProductID: productID, DeviceReleaseBCD: 0x0100,
			DeviceID: deviceID, FirmwareMajor: 1, FirmwareBuild: 1,
			HardwareMajor: 1,
		},
		USB: viipertypes.XboxOneAuthorizedUSBV1{
			MaxPower2mA: 250, OUTIntervalMS: 4, INIntervalMS: 4,
		},
		Strings: viipertypes.XboxOneAuthorizedStringsV1{
			Manufacturer: "©Microsoft Corporation", Product: product,
			Serial: serial,
		},
		Feedback: viipertypes.XboxOneAuthorizedFeedbackV1{
			Source: 1, PersonaGeneration: 1, DeviceGeneration: 1,
			TransportGeneration: 1, OwnershipEpoch: 1,
			TimeToLiveMicroseconds: 250000,
		},
		ImportDeviceID: 1, LocalTimeoutMilliseconds: 100,
	}
	createRaw, err := json.Marshal(create)
	if err != nil {
		return err
	}
	deviceRaw, err := client.request(ctx,
		fmt.Sprintf("bus/%d/add-authorized-xboxone", bus.BusID), string(createRaw))
	if err != nil {
		return fmt.Errorf("crear persona Xbox One: %w", err)
	}
	var created xboxOneCreateResponse
	if err := json.Unmarshal(deviceRaw, &created); err != nil ||
		created.DevID == "" || created.Removal == "" {
		return fmt.Errorf("respuesta de persona inválida: %s", deviceRaw)
	}
	reg := xboxOneRegistration{
		BusID: bus.BusID, DevID: created.DevID, Removal: created.Removal,
		USBIPBusID: created.USBIPBusID, RemoveMs: created.RemoveMs,
	}
	fmt.Printf("persona creada: bus=%d dev=%s vid=%04X pid=%04X producto=%q deviceID=%016X serial=%s usbip=%s\n",
		reg.BusID, reg.DevID, vendorID, productID, create.Strings.Product,
		create.Identity.DeviceID, create.Strings.Serial, reg.USBIPBusID)

	var stream net.Conn
	defer func() {
		c.removeXboxOne(client, reg, stream)
	}()

	stream, err = client.openStream(ctx, reg)
	if err != nil {
		return fmt.Errorf("abrir broker Xbox One: %w", err)
	}
	if err := writeXboxOneBroker(stream, xboxOneConsumerReady, 0, nil); err != nil {
		return fmt.Errorf("ConsumerReady: %w", err)
	}
	ready, err := readXboxOneBroker(stream)
	if err != nil || ready.typ != xboxOneConsumerAck || ready.correlation != 0 || len(ready.payload) != 0 {
		return fmt.Errorf("ConsumerReadyAck inválido: frame=%+v err=%v", ready, err)
	}
	fmt.Println("broker: ConsumerReady aceptado")
	if c.PauseBeforeActivate {
		fmt.Println("PAUSA: persona lista; ejecuta usbip.exe attach y luego Ctrl+C")
		select {}
	}

	activationPayload, _ := json.Marshal(map[string]any{
		"version": 1, "removalToken": reg.Removal,
	})
	activationRaw, err := client.request(ctx,
		fmt.Sprintf("bus/%d/%s/activate-authorized-xboxone", reg.BusID, reg.DevID),
		string(activationPayload))
	if err != nil {
		return fmt.Errorf("activar attach nativo: %w", err)
	}
	var activation xboxOneActivationResponse
	if err := json.Unmarshal(activationRaw, &activation); err != nil ||
		activation.USBIPPort <= 0 {
		return fmt.Errorf("respuesta de activación inválida: %s", activationRaw)
	}
	fmt.Printf("attach nativo aceptado: usbipBusId=%s port=%d\n",
		activation.USBIPBusID, activation.USBIPPort)

	var wire [xboxone.SemanticInputWireSize]byte
	if err := xboxone.EncodeSemanticInputWireV1Into(wire[:], xboxone.InputStateV1{}); err != nil {
		return err
	}
	// The broker starts its producer revision at 1; the first published
	// semantic state is therefore revision 2.
	if err := writeXboxOneBroker(stream, xboxOneSemanticInput, 2, wire[:]); err != nil {
		return fmt.Errorf("enviar estado neutral: %w", err)
	}
	ack, err := readXboxOneBroker(stream)
	if err != nil || ack.typ != xboxOneSemanticAck || ack.correlation != 2 ||
		len(ack.payload) != 1 || ack.payload[0] != 1 {
		return fmt.Errorf("SemanticInputAck inválido: frame=%+v err=%v", ack, err)
	}
	fmt.Println("broker: estado neutral aceptado")
	if c.InputTest {
		if err := runXboxOneInputMatrix(stream, wire[:]); err != nil {
			return err
		}
	}
	fmt.Printf("PRUEBA VIIPER Xbox One completada; manteniendo el dispositivo %d segundos para inspección\n",
		c.HoldSeconds)
	if c.HoldSeconds > 0 {
		time.Sleep(time.Duration(c.HoldSeconds) * time.Second)
	}
	return nil
}

func (c *XboxOneClient) removeXboxOne(client *xboxOneAPIClient, reg xboxOneRegistration, stream net.Conn) {
	if client == nil || reg.DevID == "" || reg.Removal == "" {
		if stream != nil {
			_ = stream.Close()
		}
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"version": 1, "removalToken": reg.Removal,
	})
	removeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	removeDone := make(chan error, 1)
	go func() {
		_, removeErr := client.request(removeCtx,
			fmt.Sprintf("bus/%d/%s/remove-authorized-xboxone", reg.BusID, reg.DevID),
			string(payload))
		removeDone <- removeErr
	}()
	if stream != nil {
		for {
			select {
			case removeErr := <-removeDone:
				if removeErr != nil {
					fmt.Fprintf(os.Stderr, "retiro Xbox One: %v\n", removeErr)
				}
				_ = stream.Close()
				return
			default:
			}
			frame, readErr := readXboxOneBroker(stream)
			if readErr != nil {
				_ = stream.Close()
				return
			}
			if frame.typ == xboxOneCanonicalFeedback {
				if err := writeXboxOneBroker(stream, xboxOneCanonicalAck,
					frame.correlation, []byte{1}); err != nil {
					_ = stream.Close()
					return
				}
			}
		}
	}
	if removeErr := <-removeDone; removeErr != nil {
		fmt.Fprintf(os.Stderr, "retiro Xbox One: %v\n", removeErr)
	}
}

func runXboxOneInputMatrix(stream net.Conn, wire []byte) error {
	type namedState struct {
		name  string
		state xboxone.InputStateV1
	}
	buttonStates := []namedState{
		{"menu", xboxone.InputStateV1{Menu: true}},
		{"view", xboxone.InputStateV1{View: true}},
		{"A", xboxone.InputStateV1{A: true}},
		{"B", xboxone.InputStateV1{B: true}},
		{"X", xboxone.InputStateV1{X: true}},
		{"Y", xboxone.InputStateV1{Y: true}},
		{"dpad-up", xboxone.InputStateV1{DPadUp: true}},
		{"dpad-down", xboxone.InputStateV1{DPadDown: true}},
		{"dpad-left", xboxone.InputStateV1{DPadLeft: true}},
		{"dpad-right", xboxone.InputStateV1{DPadRight: true}},
		{"left-bumper", xboxone.InputStateV1{LeftBumper: true}},
		{"right-bumper", xboxone.InputStateV1{RightBumper: true}},
		{"left-stick-click", xboxone.InputStateV1{LeftStickButton: true}},
		{"right-stick-click", xboxone.InputStateV1{RightStickButton: true}},
		{"guide", xboxone.InputStateV1{Guide: true}},
		{"share", xboxone.InputStateV1{Share: true}},
	}
	revision := uint64(3)
	for _, test := range buttonStates {
		if err := sendXboxOneSemanticState(stream, wire, revision, test.state); err != nil {
			return fmt.Errorf("botón %s: %w", test.name, err)
		}
		fmt.Printf("broker: button %s pressed (revision=%d)\n", test.name, revision)
		revision++
		if err := sendXboxOneSemanticState(stream, wire, revision, xboxone.InputStateV1{}); err != nil {
			return fmt.Errorf("liberación %s: %w", test.name, err)
		}
		fmt.Printf("broker: button %s released (revision=%d)\n", test.name, revision)
		revision++
	}
	motionStates := []namedState{
		{"left-trigger", xboxone.InputStateV1{LeftTrigger: 1023}},
		{"right-trigger", xboxone.InputStateV1{RightTrigger: 1023}},
		{"sticks", xboxone.InputStateV1{
			LeftStickX: -32768, LeftStickY: 32767,
			RightStickX: 16384, RightStickY: -16384,
		}},
	}
	for _, test := range motionStates {
		if err := sendXboxOneSemanticState(stream, wire, revision, test.state); err != nil {
			return fmt.Errorf("control %s: %w", test.name, err)
		}
		fmt.Printf("broker: control %s aceptado (revision=%d)\n", test.name, revision)
		revision++
	}
	return nil
}

func sendXboxOneSemanticState(stream net.Conn, wire []byte, revision uint64, state xboxone.InputStateV1) error {
	if err := xboxone.EncodeSemanticInputWireV1Into(wire, state); err != nil {
		return fmt.Errorf("codificar estado: %w", err)
	}
	if err := writeXboxOneBroker(stream, xboxOneSemanticInput, revision, wire); err != nil {
		return err
	}
	ack, err := readXboxOneBroker(stream)
	if err != nil || ack.typ != xboxOneSemanticAck || ack.correlation != revision ||
		len(ack.payload) != 1 || ack.payload[0] != 1 {
		return fmt.Errorf("SemanticInputAck inválido: frame=%+v err=%v", ack, err)
	}
	return nil
}

type xboxOneAPIClient struct {
	addr string
	key  []byte
}

func (c *xboxOneAPIClient) secure(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return nil, err
	}
	r := bufio.NewReader(conn)
	clientNonce, serverNonce, err := auth.HandleAuthHandshake(r, conn, c.key, true)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	sessionKey := auth.DeriveSessionKey(c.key, serverNonce, clientNonce)
	return auth.WrapConn(conn, sessionKey, auth.Client)
}

func (c *xboxOneAPIClient) request(ctx context.Context, path, payload string) ([]byte, error) {
	conn, err := c.secure(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	command := path
	if payload != "" {
		command += " " + payload
	}
	if _, err := io.WriteString(conn, command+"\x00"); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "{") {
		var apiErr struct {
			Status        int `json:"status"`
			Title, Detail string
		}
		if json.Unmarshal([]byte(line), &apiErr) == nil && apiErr.Status >= 400 {
			return nil, fmt.Errorf("API %d %s: %s", apiErr.Status, apiErr.Title, apiErr.Detail)
		}
	}
	return []byte(line), nil
}

func (c *xboxOneAPIClient) openStream(ctx context.Context, reg xboxOneRegistration) (net.Conn, error) {
	conn, err := c.secure(ctx)
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]any{
		"version": 1, "removalToken": reg.Removal,
	})
	command := fmt.Sprintf("bus/%d/%s/stream-authorized-xboxone %s\x00",
		reg.BusID, reg.DevID, payload)
	if _, err := io.WriteString(conn, command); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

type xboxOneBrokerFrame struct {
	typ         byte
	correlation uint64
	payload     []byte
}

func writeXboxOneBroker(w io.Writer, typ byte, correlation uint64, payload []byte) error {
	frame := make([]byte, 16+len(payload))
	copy(frame[:4], xboxOneBrokerMagic)
	frame[4] = xboxOneBrokerVersion
	frame[5] = typ
	binary.LittleEndian.PutUint16(frame[6:8], uint16(len(payload)))
	binary.LittleEndian.PutUint64(frame[8:16], correlation)
	copy(frame[16:], payload)
	_, err := w.Write(frame)
	return err
}

func readXboxOneBroker(r io.Reader) (xboxOneBrokerFrame, error) {
	var header [16]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return xboxOneBrokerFrame{}, err
	}
	if string(header[:4]) != xboxOneBrokerMagic || header[4] != xboxOneBrokerVersion {
		return xboxOneBrokerFrame{}, errors.New("encabezado X1BR inválido")
	}
	n := int(binary.LittleEndian.Uint16(header[6:8]))
	if n > 4096 {
		return xboxOneBrokerFrame{}, errors.New("payload X1BR demasiado grande")
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return xboxOneBrokerFrame{}, err
	}
	return xboxOneBrokerFrame{
		typ: header[5], correlation: binary.LittleEndian.Uint64(header[8:16]),
		payload: payload,
	}, nil
}
