// Code generated by protoc-gen-go. DO NOT EDIT.
// source: orderer/configuration.proto

package orderer

import (
	fmt "fmt"
	math "math"

	proto "github.com/golang/protobuf/proto"
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

// State defines the orderer mode of operation, typically for consensus-type migration.
// NORMAL is during normal operation, when consensus-type migration is not, and can not, take place.
// MAINTENANCE is when the consensus-type can be changed.
type ConsensusType_State int32

const (
	ConsensusType_STATE_NORMAL      ConsensusType_State = 0
	ConsensusType_STATE_MAINTENANCE ConsensusType_State = 1
)

var ConsensusType_State_name = map[int32]string{
	0: "STATE_NORMAL",
	1: "STATE_MAINTENANCE",
}

var ConsensusType_State_value = map[string]int32{
	"STATE_NORMAL":      0,
	"STATE_MAINTENANCE": 1,
}

func (x ConsensusType_State) String() string {
	return proto.EnumName(ConsensusType_State_name, int32(x))
}

func (ConsensusType_State) EnumDescriptor() ([]byte, []int) {
	return fileDescriptor_bcce68f21316dd30, []int{0, 0}
}

type ConsensusType struct {
	// The consensus type: "solo", "kafka" or "etcdraft".
	Type string `protobuf:"bytes,1,opt,name=type,proto3" json:"type,omitempty"`
	// Opaque metadata, dependent on the consensus type.
	Metadata []byte `protobuf:"bytes,2,opt,name=metadata,proto3" json:"metadata,omitempty"`
	// The state signals the ordering service to go into maintenance mode, typically for consensus-type migration.
	State                ConsensusType_State `protobuf:"varint,3,opt,name=state,proto3,enum=orderer.ConsensusType_State" json:"state,omitempty"`
	XXX_NoUnkeyedLiteral struct{}            `json:"-"`
	XXX_unrecognized     []byte              `json:"-"`
	XXX_sizecache        int32               `json:"-"`
}

func (m *ConsensusType) Reset()         { *m = ConsensusType{} }
func (m *ConsensusType) String() string { return proto.CompactTextString(m) }
func (*ConsensusType) ProtoMessage()    {}
func (*ConsensusType) Descriptor() ([]byte, []int) {
	return fileDescriptor_bcce68f21316dd30, []int{0}
}

func (m *ConsensusType) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_ConsensusType.Unmarshal(m, b)
}
func (m *ConsensusType) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_ConsensusType.Marshal(b, m, deterministic)
}
func (m *ConsensusType) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ConsensusType.Merge(m, src)
}
func (m *ConsensusType) XXX_Size() int {
	return xxx_messageInfo_ConsensusType.Size(m)
}
func (m *ConsensusType) XXX_DiscardUnknown() {
	xxx_messageInfo_ConsensusType.DiscardUnknown(m)
}

var xxx_messageInfo_ConsensusType proto.InternalMessageInfo

func (m *ConsensusType) GetType() string {
	if m != nil {
		return m.Type
	}
	return ""
}

func (m *ConsensusType) GetMetadata() []byte {
	if m != nil {
		return m.Metadata
	}
	return nil
}

func (m *ConsensusType) GetState() ConsensusType_State {
	if m != nil {
		return m.State
	}
	return ConsensusType_STATE_NORMAL
}

type BatchSize struct {
	// Simply specified as number of messages for now, in the future
	// we may want to allow this to be specified by size in bytes
	MaxMessageCount uint32 `protobuf:"varint,1,opt,name=max_message_count,json=maxMessageCount,proto3" json:"max_message_count,omitempty"`
	// The byte count of the serialized messages in a batch cannot
	// exceed this value.
	AbsoluteMaxBytes uint32 `protobuf:"varint,2,opt,name=absolute_max_bytes,json=absoluteMaxBytes,proto3" json:"absolute_max_bytes,omitempty"`
	// The byte count of the serialized messages in a batch should not
	// exceed this value.
	PreferredMaxBytes    uint32   `protobuf:"varint,3,opt,name=preferred_max_bytes,json=preferredMaxBytes,proto3" json:"preferred_max_bytes,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *BatchSize) Reset()         { *m = BatchSize{} }
func (m *BatchSize) String() string { return proto.CompactTextString(m) }
func (*BatchSize) ProtoMessage()    {}
func (*BatchSize) Descriptor() ([]byte, []int) {
	return fileDescriptor_bcce68f21316dd30, []int{1}
}

func (m *BatchSize) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_BatchSize.Unmarshal(m, b)
}
func (m *BatchSize) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_BatchSize.Marshal(b, m, deterministic)
}
func (m *BatchSize) XXX_Merge(src proto.Message) {
	xxx_messageInfo_BatchSize.Merge(m, src)
}
func (m *BatchSize) XXX_Size() int {
	return xxx_messageInfo_BatchSize.Size(m)
}
func (m *BatchSize) XXX_DiscardUnknown() {
	xxx_messageInfo_BatchSize.DiscardUnknown(m)
}

var xxx_messageInfo_BatchSize proto.InternalMessageInfo

func (m *BatchSize) GetMaxMessageCount() uint32 {
	if m != nil {
		return m.MaxMessageCount
	}
	return 0
}

func (m *BatchSize) GetAbsoluteMaxBytes() uint32 {
	if m != nil {
		return m.AbsoluteMaxBytes
	}
	return 0
}

func (m *BatchSize) GetPreferredMaxBytes() uint32 {
	if m != nil {
		return m.PreferredMaxBytes
	}
	return 0
}

type BatchTimeout struct {
	// Any duration string parseable by ParseDuration():
	// https://golang.org/pkg/time/#ParseDuration
	Timeout              string   `protobuf:"bytes,1,opt,name=timeout,proto3" json:"timeout,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *BatchTimeout) Reset()         { *m = BatchTimeout{} }
func (m *BatchTimeout) String() string { return proto.CompactTextString(m) }
func (*BatchTimeout) ProtoMessage()    {}
func (*BatchTimeout) Descriptor() ([]byte, []int) {
	return fileDescriptor_bcce68f21316dd30, []int{2}
}

func (m *BatchTimeout) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_BatchTimeout.Unmarshal(m, b)
}
func (m *BatchTimeout) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_BatchTimeout.Marshal(b, m, deterministic)
}
func (m *BatchTimeout) XXX_Merge(src proto.Message) {
	xxx_messageInfo_BatchTimeout.Merge(m, src)
}
func (m *BatchTimeout) XXX_Size() int {
	return xxx_messageInfo_BatchTimeout.Size(m)
}
func (m *BatchTimeout) XXX_DiscardUnknown() {
	xxx_messageInfo_BatchTimeout.DiscardUnknown(m)
}

var xxx_messageInfo_BatchTimeout proto.InternalMessageInfo

func (m *BatchTimeout) GetTimeout() string {
	if m != nil {
		return m.Timeout
	}
	return ""
}

// Carries a list of bootstrap brokers, i.e. this is not the exclusive set of
// brokers an ordering service
type KafkaBrokers struct {
	// Each broker here should be identified using the (IP|host):port notation,
	// e.g. 127.0.0.1:7050, or localhost:7050 are valid entries
	Brokers              []string `protobuf:"bytes,1,rep,name=brokers,proto3" json:"brokers,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *KafkaBrokers) Reset()         { *m = KafkaBrokers{} }
func (m *KafkaBrokers) String() string { return proto.CompactTextString(m) }
func (*KafkaBrokers) ProtoMessage()    {}
func (*KafkaBrokers) Descriptor() ([]byte, []int) {
	return fileDescriptor_bcce68f21316dd30, []int{3}
}

func (m *KafkaBrokers) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_KafkaBrokers.Unmarshal(m, b)
}
func (m *KafkaBrokers) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_KafkaBrokers.Marshal(b, m, deterministic)
}
func (m *KafkaBrokers) XXX_Merge(src proto.Message) {
	xxx_messageInfo_KafkaBrokers.Merge(m, src)
}
func (m *KafkaBrokers) XXX_Size() int {
	return xxx_messageInfo_KafkaBrokers.Size(m)
}
func (m *KafkaBrokers) XXX_DiscardUnknown() {
	xxx_messageInfo_KafkaBrokers.DiscardUnknown(m)
}

var xxx_messageInfo_KafkaBrokers proto.InternalMessageInfo

func (m *KafkaBrokers) GetBrokers() []string {
	if m != nil {
		return m.Brokers
	}
	return nil
}

// ChannelRestrictions is the mssage which conveys restrictions on channel creation for an orderer
type ChannelRestrictions struct {
	MaxCount             uint64   `protobuf:"varint,1,opt,name=max_count,json=maxCount,proto3" json:"max_count,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *ChannelRestrictions) Reset()         { *m = ChannelRestrictions{} }
func (m *ChannelRestrictions) String() string { return proto.CompactTextString(m) }
func (*ChannelRestrictions) ProtoMessage()    {}
func (*ChannelRestrictions) Descriptor() ([]byte, []int) {
	return fileDescriptor_bcce68f21316dd30, []int{4}
}

func (m *ChannelRestrictions) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_ChannelRestrictions.Unmarshal(m, b)
}
func (m *ChannelRestrictions) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_ChannelRestrictions.Marshal(b, m, deterministic)
}
func (m *ChannelRestrictions) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ChannelRestrictions.Merge(m, src)
}
func (m *ChannelRestrictions) XXX_Size() int {
	return xxx_messageInfo_ChannelRestrictions.Size(m)
}
func (m *ChannelRestrictions) XXX_DiscardUnknown() {
	xxx_messageInfo_ChannelRestrictions.DiscardUnknown(m)
}

var xxx_messageInfo_ChannelRestrictions proto.InternalMessageInfo

func (m *ChannelRestrictions) GetMaxCount() uint64 {
	if m != nil {
		return m.MaxCount
	}
	return 0
}

func init() {
	proto.RegisterEnum("orderer.ConsensusType_State", ConsensusType_State_name, ConsensusType_State_value)
	proto.RegisterType((*ConsensusType)(nil), "orderer.ConsensusType")
	proto.RegisterType((*BatchSize)(nil), "orderer.BatchSize")
	proto.RegisterType((*BatchTimeout)(nil), "orderer.BatchTimeout")
	proto.RegisterType((*KafkaBrokers)(nil), "orderer.KafkaBrokers")
	proto.RegisterType((*ChannelRestrictions)(nil), "orderer.ChannelRestrictions")
}

func init() { proto.RegisterFile("orderer/configuration.proto", fileDescriptor_bcce68f21316dd30) }

var fileDescriptor_bcce68f21316dd30 = []byte{
	// 406 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x54, 0x91, 0x51, 0x8b, 0xda, 0x40,
	0x10, 0xc7, 0x9b, 0x7a, 0xd7, 0x3b, 0x07, 0x6d, 0x75, 0x8f, 0x42, 0xe8, 0xf5, 0x41, 0x02, 0x05,
	0x29, 0xbd, 0x4d, 0xb1, 0x9f, 0x40, 0xc5, 0x87, 0xd2, 0x6a, 0x61, 0xcd, 0x43, 0xe9, 0x8b, 0x4c,
	0xe2, 0x18, 0xc3, 0x99, 0x6c, 0xd8, 0xdd, 0x80, 0xf6, 0x7b, 0xf4, 0x23, 0xf4, 0x7b, 0x96, 0xdd,
	0x8d, 0x57, 0xef, 0x6d, 0xfe, 0xff, 0xf9, 0xed, 0x30, 0xb3, 0x7f, 0xb8, 0x97, 0x6a, 0x4b, 0x8a,
	0x54, 0x9c, 0xc9, 0x6a, 0x57, 0xe4, 0x8d, 0x42, 0x53, 0xc8, 0x8a, 0xd7, 0x4a, 0x1a, 0xc9, 0x6e,
	0xda, 0x66, 0xf4, 0x37, 0x80, 0xfe, 0x5c, 0x56, 0x9a, 0x2a, 0xdd, 0xe8, 0xe4, 0x54, 0x13, 0x63,
	0x70, 0x65, 0x4e, 0x35, 0x85, 0xc1, 0x28, 0x18, 0x77, 0x85, 0xab, 0xd9, 0x3b, 0xb8, 0x2d, 0xc9,
	0xe0, 0x16, 0x0d, 0x86, 0x2f, 0x47, 0xc1, 0xb8, 0x27, 0x9e, 0x34, 0x9b, 0xc0, 0xb5, 0x36, 0x68,
	0x28, 0xec, 0x8c, 0x82, 0xf1, 0xeb, 0xc9, 0x7b, 0xde, 0x8e, 0xe6, 0xcf, 0xc6, 0xf2, 0xb5, 0x65,
	0x84, 0x47, 0xa3, 0xcf, 0x70, 0xed, 0x34, 0x1b, 0x40, 0x6f, 0x9d, 0x4c, 0x93, 0xc5, 0x66, 0xf5,
	0x43, 0x2c, 0xa7, 0xdf, 0x07, 0x2f, 0xd8, 0x5b, 0x18, 0x7a, 0x67, 0x39, 0xfd, 0xba, 0x4a, 0x16,
	0xab, 0xe9, 0x6a, 0xbe, 0x18, 0x04, 0xd1, 0x9f, 0x00, 0xba, 0x33, 0x34, 0xd9, 0x7e, 0x5d, 0xfc,
	0x26, 0xf6, 0x11, 0x86, 0x25, 0x1e, 0x37, 0x25, 0x69, 0x8d, 0x39, 0x6d, 0x32, 0xd9, 0x54, 0xc6,
	0x2d, 0xdc, 0x17, 0x6f, 0x4a, 0x3c, 0x2e, 0xbd, 0x3f, 0xb7, 0x36, 0xfb, 0x04, 0x0c, 0x53, 0x2d,
	0x0f, 0x8d, 0xa1, 0x8d, 0x7d, 0x94, 0x9e, 0x0c, 0x69, 0x77, 0x45, 0x5f, 0x0c, 0xce, 0x9d, 0x25,
	0x1e, 0x67, 0xd6, 0x67, 0x1c, 0xee, 0x6a, 0x45, 0x3b, 0x52, 0x8a, 0xb6, 0x17, 0x78, 0xc7, 0xe1,
	0xc3, 0xa7, 0xd6, 0x99, 0x8f, 0xc6, 0xd0, 0x73, 0x6b, 0x25, 0x45, 0x49, 0xb2, 0x31, 0x2c, 0x84,
	0x1b, 0xe3, 0xcb, 0xf6, 0x03, 0xcf, 0xd2, 0x92, 0xdf, 0x70, 0xf7, 0x88, 0x33, 0x25, 0x1f, 0x49,
	0x69, 0x4b, 0xa6, 0xbe, 0x0c, 0x83, 0x51, 0xc7, 0x92, 0xad, 0x8c, 0x26, 0x70, 0x37, 0xdf, 0x63,
	0x55, 0xd1, 0x41, 0x90, 0x36, 0xaa, 0xc8, 0x6c, 0x70, 0x9a, 0xdd, 0x43, 0xd7, 0x2e, 0xf4, 0xff,
	0xd8, 0x2b, 0x71, 0x5b, 0xe2, 0xd1, 0x5d, 0x39, 0xfb, 0x09, 0x1f, 0xa4, 0xca, 0xf9, 0xfe, 0x54,
	0x93, 0x3a, 0xd0, 0x36, 0x27, 0xc5, 0x77, 0x98, 0xaa, 0x22, 0xf3, 0x81, 0xeb, 0x73, 0x2a, 0xbf,
	0xe2, 0xbc, 0x30, 0xfb, 0x26, 0xe5, 0x99, 0x2c, 0xe3, 0x0b, 0x3a, 0xf6, 0xf4, 0x83, 0xa7, 0x1f,
	0x72, 0x19, 0xb7, 0x0f, 0xd2, 0x57, 0xce, 0xfa, 0xf2, 0x2f, 0x00, 0x00, 0xff, 0xff, 0x97, 0x3c,
	0x9e, 0x25, 0x50, 0x02, 0x00, 0x00,
}
