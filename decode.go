package minimp3

/*
#define MINIMP3_IMPLEMENTATION

#include "minimp3.h"
#include <stdlib.h>
#include <stdio.h>

int decode(mp3dec_t *dec, mp3dec_frame_info_t *info, unsigned char *data, int *length, unsigned char *decoded, int *decoded_length) {
    int samples;
    short pcm[MINIMP3_MAX_SAMPLES_PER_FRAME];
    samples = mp3dec_decode_frame(dec, data, *length, pcm, info);
    *decoded_length = samples * info->channels * 2;
    *length -= info->frame_bytes;
    unsigned char buffer[samples * info->channels * 2];
    memcpy(buffer, (unsigned char*)&(pcm), sizeof(short) * samples * info->channels);
    memcpy(decoded, buffer, sizeof(short) * samples * info->channels);
    return info->frame_bytes;
}
*/
import "C"
import (
	"context"
	"io"
	"sync"
	"time"
	"unsafe"
)

const maxSamplesPerFrame = 1152 * 2

// Decoder decode the mp3 stream by minimp3
type Decoder struct {
	reader      io.ReadCloser
	data        []byte
	lockData    sync.RWMutex
	decodedData []byte
	decode      C.mp3dec_t
	info        C.mp3dec_frame_info_t
	SampleRate  int
	Channels    int
	Kbps        int
	Layer       int
	closed      bool
}

// BufferSize Decoded data buffer size.
var BufferSize = 1024 * 100

// WaitForDataDuration wait for the data time duration.
var WaitForDataDuration = time.Millisecond * 10

// NewDecoder decode mp3 stream and get a Decoder for read the raw data to play.
func NewDecoder(reader io.ReadCloser) (*Decoder, error) {
	dec := Decoder{
		reader: reader,
		decode: C.mp3dec_t{},
	}

	C.mp3dec_init(&dec.decode)
	dec.info = C.mp3dec_frame_info_t{}

	return &dec, nil
}

// Start begins decoding MP3 data as long a the context is valid
// the returned channel is closed after enough data is initial processed
func (dec *Decoder) Start(ctx context.Context) chan bool {
	channel := make(chan bool)
	decCtx, cancel := context.WithCancel(ctx)

	go func() {
		defer cancel()
		defer func() {
			dec.closed = true
		}()

		for {
			select {
			case <-decCtx.Done():
				break
			default:
			}
			if len(dec.data) > BufferSize {
				<-time.After(WaitForDataDuration)
				continue
			}
			var data = make([]byte, 512)
			var n int
			n, err := dec.reader.Read(data)
			if err == io.EOF {
				return
			}
			if err != nil {
				return
			}

			dec.lockData.Lock()
			dec.data = append(dec.data, data[:n]...)
			dec.lockData.Unlock()
		}
	}()

	go func() {
		for {
			select {
			case <-decCtx.Done():
				break
			default:
			}
			if len(dec.decodedData) > BufferSize {
				<-time.After(WaitForDataDuration)
				continue
			}
			var decoded = [maxSamplesPerFrame * 2]byte{}
			var decodedLength = C.int(0)
			var length = C.int(len(dec.data))
			if len(dec.data) == 0 {
				<-time.After(WaitForDataDuration)
				continue
			}

			frameSize := C.decode(&dec.decode, &dec.info,
				(*C.uchar)(unsafe.Pointer(&dec.data[0])),
				&length, (*C.uchar)(unsafe.Pointer(&decoded[0])),
				&decodedLength)
			if int(frameSize) == 0 {
				<-time.After(WaitForDataDuration)
				continue
			}

			dec.SampleRate = int(dec.info.hz)
			dec.Channels = int(dec.info.channels)
			dec.Kbps = int(dec.info.bitrate_kbps)
			dec.Layer = int(dec.info.layer)
			dec.lockData.Lock()
			dec.decodedData = append(dec.decodedData, decoded[:decodedLength]...)
			if int(frameSize) < len(dec.data) {
				dec.data = dec.data[int(frameSize):]
			}
			dec.lockData.Unlock()
		}
	}()

	go func() {
		for {
			select {
			case <-decCtx.Done():
				close(channel)
			default:
			}
			if len(dec.decodedData) != 0 {
				close(channel)
				return
			} else {
				<-time.After(time.Millisecond * 100)
			}
		}
	}()

	return channel
}

// Read read raw stream into data
func (dec *Decoder) Read(data []byte) (n int, err error) {
	if dec.closed {
		return 0, io.EOF
	}
	dec.lockData.Lock()
	defer dec.lockData.Unlock()
	if len(dec.decodedData) == 0 {
		return
	}
	n = copy(data, dec.decodedData[:])
	dec.decodedData = dec.decodedData[n:]
	return
}

// Close stop the decode mp3 stream cycle.
func (dec *Decoder) Close() {
	dec.reader.Close()
}

// DecodeFull put all of the mp3 data to decode.
func DecodeFull(mp3 []byte) (dec *Decoder, decodedData []byte, err error) {
	dec = new(Decoder)
	dec.decode = C.mp3dec_t{}
	C.mp3dec_init(&dec.decode)
	info := C.mp3dec_frame_info_t{}
	var length = C.int(len(mp3))
	for {
		var decoded = [maxSamplesPerFrame * 2]byte{}
		var decodedLength = C.int(0)
		frameSize := C.decode(&dec.decode,
			&info, (*C.uchar)(unsafe.Pointer(&mp3[0])),
			&length, (*C.uchar)(unsafe.Pointer(&decoded[0])),
			&decodedLength)
		if int(frameSize) == 0 {
			break
		}
		decodedData = append(decodedData, decoded[:decodedLength]...)
		if int(frameSize) < len(mp3) {
			mp3 = mp3[int(frameSize):]
		}
		dec.SampleRate = int(info.hz)
		dec.Channels = int(info.channels)
		dec.Kbps = int(info.bitrate_kbps)
		dec.Layer = int(info.layer)
	}
	return
}
