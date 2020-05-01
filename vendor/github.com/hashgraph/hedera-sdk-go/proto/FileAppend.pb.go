// Code generated by protoc-gen-go. DO NOT EDIT.
// source: proto/FileAppend.proto

package proto

import (
	fmt "fmt"
	proto "github.com/golang/protobuf/proto"
	math "math"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion3 // please upgrade the proto package

// Append the given contents to the end of the file. If a file is too big to create with a single FileCreateTransaction, then it can be created with the first part of its contents, and then appended multiple times to create the entire file.
type FileAppendTransactionBody struct {
	FileID               *FileID  `protobuf:"bytes,2,opt,name=fileID,proto3" json:"fileID,omitempty"`
	Contents             []byte   `protobuf:"bytes,4,opt,name=contents,proto3" json:"contents,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *FileAppendTransactionBody) Reset()         { *m = FileAppendTransactionBody{} }
func (m *FileAppendTransactionBody) String() string { return proto.CompactTextString(m) }
func (*FileAppendTransactionBody) ProtoMessage()    {}
func (*FileAppendTransactionBody) Descriptor() ([]byte, []int) {
	return fileDescriptor_aa305bc718d1bab1, []int{0}
}

func (m *FileAppendTransactionBody) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_FileAppendTransactionBody.Unmarshal(m, b)
}
func (m *FileAppendTransactionBody) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_FileAppendTransactionBody.Marshal(b, m, deterministic)
}
func (m *FileAppendTransactionBody) XXX_Merge(src proto.Message) {
	xxx_messageInfo_FileAppendTransactionBody.Merge(m, src)
}
func (m *FileAppendTransactionBody) XXX_Size() int {
	return xxx_messageInfo_FileAppendTransactionBody.Size(m)
}
func (m *FileAppendTransactionBody) XXX_DiscardUnknown() {
	xxx_messageInfo_FileAppendTransactionBody.DiscardUnknown(m)
}

var xxx_messageInfo_FileAppendTransactionBody proto.InternalMessageInfo

func (m *FileAppendTransactionBody) GetFileID() *FileID {
	if m != nil {
		return m.FileID
	}
	return nil
}

func (m *FileAppendTransactionBody) GetContents() []byte {
	if m != nil {
		return m.Contents
	}
	return nil
}

func init() {
	proto.RegisterType((*FileAppendTransactionBody)(nil), "proto.FileAppendTransactionBody")
}

func init() {
	proto.RegisterFile("proto/FileAppend.proto", fileDescriptor_aa305bc718d1bab1)
}

var fileDescriptor_aa305bc718d1bab1 = []byte{
	// 192 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xe2, 0x12, 0x2b, 0x28, 0xca, 0x2f,
	0xc9, 0xd7, 0x77, 0xcb, 0xcc, 0x49, 0x75, 0x2c, 0x28, 0x48, 0xcd, 0x4b, 0xd1, 0x03, 0x0b, 0x08,
	0xb1, 0x82, 0x29, 0x29, 0xa8, 0xb4, 0x53, 0x62, 0x71, 0x66, 0x72, 0x48, 0x65, 0x41, 0x6a, 0x31,
	0x44, 0x5a, 0x29, 0x8e, 0x4b, 0x12, 0xa1, 0x25, 0xa4, 0x28, 0x31, 0xaf, 0x38, 0x31, 0xb9, 0x24,
	0x33, 0x3f, 0xcf, 0x29, 0x3f, 0xa5, 0x52, 0x48, 0x95, 0x8b, 0x2d, 0x2d, 0x33, 0x27, 0xd5, 0xd3,
	0x45, 0x82, 0x49, 0x81, 0x51, 0x83, 0xdb, 0x88, 0x17, 0xa2, 0x49, 0xcf, 0x0d, 0x2c, 0x18, 0x04,
	0x95, 0x14, 0x92, 0xe2, 0xe2, 0x48, 0xce, 0xcf, 0x2b, 0x49, 0xcd, 0x2b, 0x29, 0x96, 0x60, 0x51,
	0x60, 0xd4, 0xe0, 0x09, 0x82, 0xf3, 0x9d, 0x3c, 0xb8, 0xa4, 0x92, 0xf3, 0x73, 0xf5, 0x32, 0x52,
	0x53, 0x52, 0x8b, 0x12, 0xf5, 0x32, 0x12, 0x8b, 0x33, 0xd2, 0x8b, 0x12, 0x0b, 0x32, 0x20, 0x06,
	0x05, 0x30, 0x46, 0x69, 0xa4, 0x67, 0x96, 0x64, 0x94, 0x26, 0xe9, 0x25, 0xe7, 0xe7, 0xea, 0xc3,
	0x65, 0xf5, 0x21, 0xca, 0x75, 0x8b, 0x53, 0xb2, 0x75, 0xd3, 0xf3, 0xf5, 0xc1, 0x6a, 0x93, 0xd8,
	0xc0, 0x94, 0x31, 0x20, 0x00, 0x00, 0xff, 0xff, 0x3a, 0xba, 0xc3, 0xaa, 0xe9, 0x00, 0x00, 0x00,
}
