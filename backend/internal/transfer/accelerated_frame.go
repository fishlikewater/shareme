package transfer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	acceleratedFrameMagic      uint32 = 0x4d534146
	acceleratedFrameKindHello  byte   = 1
	acceleratedFrameKindData   byte   = 2
	acceleratedFrameKindAck    byte   = 3
	acceleratedMaxControlBytes        = 4096
	acceleratedMaxPayloadBytes        = 16 << 20
)

var ErrAcceleratedInvalidFrame = errors.New("invalid accelerated frame")

type AcceleratedHelloFrame struct {
	SessionID     string
	TransferToken string
	LaneIndex     int
}

type AcceleratedDataFrame struct {
	Offset  int64
	Payload []byte
}

type AcceleratedAckFrame struct {
	Offset int64
	Length int64
}

func WriteAcceleratedHello(writer io.Writer, frame AcceleratedHelloFrame) error {
	sessionID := []byte(frame.SessionID)
	transferToken := []byte(frame.TransferToken)
	if len(sessionID) == 0 || len(transferToken) == 0 {
		return fmt.Errorf("%w: session metadata required", ErrAcceleratedInvalidFrame)
	}
	if len(sessionID) > acceleratedMaxControlBytes || len(transferToken) > acceleratedMaxControlBytes {
		return fmt.Errorf("%w: control payload too large", ErrAcceleratedInvalidFrame)
	}
	if frame.LaneIndex < 0 || frame.LaneIndex > 0xffff {
		return fmt.Errorf("%w: lane index out of range", ErrAcceleratedInvalidFrame)
	}

	var header [11]byte
	binary.BigEndian.PutUint32(header[0:4], acceleratedFrameMagic)
	header[4] = acceleratedFrameKindHello
	binary.BigEndian.PutUint16(header[5:7], uint16(len(sessionID)))
	binary.BigEndian.PutUint16(header[7:9], uint16(len(transferToken)))
	binary.BigEndian.PutUint16(header[9:11], uint16(frame.LaneIndex))

	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	if _, err := writer.Write(sessionID); err != nil {
		return err
	}
	if _, err := writer.Write(transferToken); err != nil {
		return err
	}
	return nil
}

func ReadAcceleratedHello(reader io.Reader) (AcceleratedHelloFrame, error) {
	var header [11]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return AcceleratedHelloFrame{}, err
	}
	if binary.BigEndian.Uint32(header[0:4]) != acceleratedFrameMagic {
		return AcceleratedHelloFrame{}, fmt.Errorf("%w: unexpected hello magic", ErrAcceleratedInvalidFrame)
	}
	if header[4] != acceleratedFrameKindHello {
		return AcceleratedHelloFrame{}, fmt.Errorf("%w: unexpected hello frame kind %d", ErrAcceleratedInvalidFrame, header[4])
	}

	sessionLength := int(binary.BigEndian.Uint16(header[5:7]))
	tokenLength := int(binary.BigEndian.Uint16(header[7:9]))
	if sessionLength == 0 || tokenLength == 0 {
		return AcceleratedHelloFrame{}, fmt.Errorf("%w: session metadata required", ErrAcceleratedInvalidFrame)
	}

	sessionID := make([]byte, sessionLength)
	if _, err := io.ReadFull(reader, sessionID); err != nil {
		return AcceleratedHelloFrame{}, err
	}
	transferToken := make([]byte, tokenLength)
	if _, err := io.ReadFull(reader, transferToken); err != nil {
		return AcceleratedHelloFrame{}, err
	}

	return AcceleratedHelloFrame{
		SessionID:     string(sessionID),
		TransferToken: string(transferToken),
		LaneIndex:     int(binary.BigEndian.Uint16(header[9:11])),
	}, nil
}

func WriteAcceleratedDataFrame(writer io.Writer, frame AcceleratedDataFrame) error {
	if frame.Offset < 0 {
		return fmt.Errorf("%w: negative offset", ErrAcceleratedInvalidFrame)
	}
	if len(frame.Payload) == 0 || len(frame.Payload) > acceleratedMaxPayloadBytes {
		return fmt.Errorf("%w: invalid payload length %d", ErrAcceleratedInvalidFrame, len(frame.Payload))
	}

	var header [17]byte
	binary.BigEndian.PutUint32(header[0:4], acceleratedFrameMagic)
	header[4] = acceleratedFrameKindData
	binary.BigEndian.PutUint64(header[5:13], uint64(frame.Offset))
	binary.BigEndian.PutUint32(header[13:17], uint32(len(frame.Payload)))

	if _, err := writer.Write(header[:]); err != nil {
		return err
	}
	if _, err := writer.Write(frame.Payload); err != nil {
		return err
	}
	return nil
}

func ReadAcceleratedDataFrame(reader io.Reader) (AcceleratedDataFrame, error) {
	var header [17]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return AcceleratedDataFrame{}, io.EOF
		}
		return AcceleratedDataFrame{}, err
	}
	if binary.BigEndian.Uint32(header[0:4]) != acceleratedFrameMagic {
		return AcceleratedDataFrame{}, fmt.Errorf("%w: unexpected data magic", ErrAcceleratedInvalidFrame)
	}
	if header[4] != acceleratedFrameKindData {
		return AcceleratedDataFrame{}, fmt.Errorf("%w: unexpected data frame kind %d", ErrAcceleratedInvalidFrame, header[4])
	}

	payloadLength := int(binary.BigEndian.Uint32(header[13:17]))
	if payloadLength == 0 || payloadLength > acceleratedMaxPayloadBytes {
		return AcceleratedDataFrame{}, fmt.Errorf("%w: invalid payload length %d", ErrAcceleratedInvalidFrame, payloadLength)
	}

	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return AcceleratedDataFrame{}, err
	}

	return AcceleratedDataFrame{
		Offset:  int64(binary.BigEndian.Uint64(header[5:13])),
		Payload: payload,
	}, nil
}

func WriteAcceleratedAckFrame(writer io.Writer, frame AcceleratedAckFrame) error {
	if frame.Offset < 0 {
		return fmt.Errorf("%w: negative ack offset", ErrAcceleratedInvalidFrame)
	}
	if frame.Length <= 0 || frame.Length > acceleratedMaxPayloadBytes {
		return fmt.Errorf("%w: invalid ack length %d", ErrAcceleratedInvalidFrame, frame.Length)
	}

	var header [17]byte
	binary.BigEndian.PutUint32(header[0:4], acceleratedFrameMagic)
	header[4] = acceleratedFrameKindAck
	binary.BigEndian.PutUint64(header[5:13], uint64(frame.Offset))
	binary.BigEndian.PutUint32(header[13:17], uint32(frame.Length))

	_, err := writer.Write(header[:])
	return err
}

func ReadAcceleratedAckFrame(reader io.Reader) (AcceleratedAckFrame, error) {
	var header [17]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return AcceleratedAckFrame{}, err
	}
	if binary.BigEndian.Uint32(header[0:4]) != acceleratedFrameMagic {
		return AcceleratedAckFrame{}, fmt.Errorf("%w: unexpected ack magic", ErrAcceleratedInvalidFrame)
	}
	if header[4] != acceleratedFrameKindAck {
		return AcceleratedAckFrame{}, fmt.Errorf("%w: unexpected ack frame kind %d", ErrAcceleratedInvalidFrame, header[4])
	}

	length := int64(binary.BigEndian.Uint32(header[13:17]))
	if length <= 0 || length > acceleratedMaxPayloadBytes {
		return AcceleratedAckFrame{}, fmt.Errorf("%w: invalid ack length %d", ErrAcceleratedInvalidFrame, length)
	}

	return AcceleratedAckFrame{
		Offset: int64(binary.BigEndian.Uint64(header[5:13])),
		Length: length,
	}, nil
}
