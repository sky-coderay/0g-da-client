package encoder

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/0glabs/0g-da-client/common"
	"github.com/0glabs/0g-da-client/core"
	"github.com/0glabs/0g-da-client/disperser"
	pb "github.com/0glabs/0g-da-client/disperser/api/grpc/encoder"
	bn "github.com/consensys/gnark-crypto/ecc/bn254"
	"github.com/consensys/gnark-crypto/ecc/bn254/fp"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type client struct {
	addr    string
	timeout time.Duration
}

func NewEncoderClient(addr string, timeout time.Duration) (disperser.EncoderClient, error) {
	return client{
		addr:    addr,
		timeout: timeout,
	}, nil
}

func (c client) EncodeBlob(ctx context.Context, data []byte, log common.Logger) (*core.BlobCommitments, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	conn, err := grpc.DialContext(
		ctxWithTimeout,
		c.addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1024*1024*1024)), // 1 GiB
	)
	if err != nil {
		return nil, fmt.Errorf("failed to dial encoder: %w", err)
	}
	defer conn.Close()

	encoder := pb.NewEncoderClient(conn)
	encodeBlobReply, err := encoder.EncodeBlob(ctx, &pb.EncodeBlobRequest{
		Data:        data,
		RequireData: false,
	})
	if err != nil {
		return nil, err
	}

	// little endian to big endian
	commitment := encodeBlobReply.GetErasureCommitment()
	if len(commitment) != bn.SizeOfG1AffineUncompressed {
		return nil, io.ErrShortBuffer
	}

	commitment[bn.SizeOfG1AffineUncompressed-1] &= 63
	for i := 0; i < fp.Bytes/2; i++ {
		commitment[i], commitment[fp.Bytes-i-1] = commitment[fp.Bytes-i-1], commitment[i]
	}

	for i := fp.Bytes; i < fp.Bytes+fp.Bytes/2; i++ {
		commitment[i], commitment[len(commitment)-(i-fp.Bytes)-1] = commitment[len(commitment)-(i-fp.Bytes)-1], commitment[i]
	}

	log.Debug("blob erasure commit", "commit", hexutil.Encode(commitment))

	commitmentPoint, err := new(core.G1Point).Deserialize(commitment)
	if err != nil {
		return nil, err
	}

	return &core.BlobCommitments{
		ErasureCommitment: commitmentPoint,
		StorageRoot:       encodeBlobReply.GetStorageRoot(),
		EncodedData:       encodeBlobReply.GetEncodedData(),
		EncodedSlice:      encodeBlobReply.GetEncodedSlice(),
	}, nil
}
