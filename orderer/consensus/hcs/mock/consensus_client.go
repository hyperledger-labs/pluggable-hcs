// Code generated by counterfeiter. DO NOT EDIT.
package mock

import (
	"sync"

	hedera "github.com/hashgraph/hedera-sdk-go"
)

type ConsensusClient struct {
	CloseStub        func() error
	closeMutex       sync.RWMutex
	closeArgsForCall []struct {
	}
	closeReturns struct {
		result1 error
	}
	closeReturnsOnCall map[int]struct {
		result1 error
	}
	GetAccountBalanceStub        func(*hedera.AccountID) (hedera.Hbar, error)
	getAccountBalanceMutex       sync.RWMutex
	getAccountBalanceArgsForCall []struct {
		arg1 *hedera.AccountID
	}
	getAccountBalanceReturns struct {
		result1 hedera.Hbar
		result2 error
	}
	getAccountBalanceReturnsOnCall map[int]struct {
		result1 hedera.Hbar
		result2 error
	}
	GetConsensusTopicInfoStub        func(*hedera.ConsensusTopicID) (*hedera.ConsensusTopicInfo, error)
	getConsensusTopicInfoMutex       sync.RWMutex
	getConsensusTopicInfoArgsForCall []struct {
		arg1 *hedera.ConsensusTopicID
	}
	getConsensusTopicInfoReturns struct {
		result1 *hedera.ConsensusTopicInfo
		result2 error
	}
	getConsensusTopicInfoReturnsOnCall map[int]struct {
		result1 *hedera.ConsensusTopicInfo
		result2 error
	}
	GetTransactionReceiptStub        func(*hedera.TransactionID) (*hedera.TransactionReceipt, error)
	getTransactionReceiptMutex       sync.RWMutex
	getTransactionReceiptArgsForCall []struct {
		arg1 *hedera.TransactionID
	}
	getTransactionReceiptReturns struct {
		result1 *hedera.TransactionReceipt
		result2 error
	}
	getTransactionReceiptReturnsOnCall map[int]struct {
		result1 *hedera.TransactionReceipt
		result2 error
	}
	SubmitConsensusMessageStub        func([]byte, *hedera.ConsensusTopicID) (*hedera.TransactionID, error)
	submitConsensusMessageMutex       sync.RWMutex
	submitConsensusMessageArgsForCall []struct {
		arg1 []byte
		arg2 *hedera.ConsensusTopicID
	}
	submitConsensusMessageReturns struct {
		result1 *hedera.TransactionID
		result2 error
	}
	submitConsensusMessageReturnsOnCall map[int]struct {
		result1 *hedera.TransactionID
		result2 error
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *ConsensusClient) Close() error {
	fake.closeMutex.Lock()
	ret, specificReturn := fake.closeReturnsOnCall[len(fake.closeArgsForCall)]
	fake.closeArgsForCall = append(fake.closeArgsForCall, struct {
	}{})
	fake.recordInvocation("Close", []interface{}{})
	fake.closeMutex.Unlock()
	if fake.CloseStub != nil {
		return fake.CloseStub()
	}
	if specificReturn {
		return ret.result1
	}
	fakeReturns := fake.closeReturns
	return fakeReturns.result1
}

func (fake *ConsensusClient) CloseCallCount() int {
	fake.closeMutex.RLock()
	defer fake.closeMutex.RUnlock()
	return len(fake.closeArgsForCall)
}

func (fake *ConsensusClient) CloseCalls(stub func() error) {
	fake.closeMutex.Lock()
	defer fake.closeMutex.Unlock()
	fake.CloseStub = stub
}

func (fake *ConsensusClient) CloseReturns(result1 error) {
	fake.closeMutex.Lock()
	defer fake.closeMutex.Unlock()
	fake.CloseStub = nil
	fake.closeReturns = struct {
		result1 error
	}{result1}
}

func (fake *ConsensusClient) CloseReturnsOnCall(i int, result1 error) {
	fake.closeMutex.Lock()
	defer fake.closeMutex.Unlock()
	fake.CloseStub = nil
	if fake.closeReturnsOnCall == nil {
		fake.closeReturnsOnCall = make(map[int]struct {
			result1 error
		})
	}
	fake.closeReturnsOnCall[i] = struct {
		result1 error
	}{result1}
}

func (fake *ConsensusClient) GetAccountBalance(arg1 *hedera.AccountID) (hedera.Hbar, error) {
	fake.getAccountBalanceMutex.Lock()
	ret, specificReturn := fake.getAccountBalanceReturnsOnCall[len(fake.getAccountBalanceArgsForCall)]
	fake.getAccountBalanceArgsForCall = append(fake.getAccountBalanceArgsForCall, struct {
		arg1 *hedera.AccountID
	}{arg1})
	fake.recordInvocation("GetAccountBalance", []interface{}{arg1})
	fake.getAccountBalanceMutex.Unlock()
	if fake.GetAccountBalanceStub != nil {
		return fake.GetAccountBalanceStub(arg1)
	}
	if specificReturn {
		return ret.result1, ret.result2
	}
	fakeReturns := fake.getAccountBalanceReturns
	return fakeReturns.result1, fakeReturns.result2
}

func (fake *ConsensusClient) GetAccountBalanceCallCount() int {
	fake.getAccountBalanceMutex.RLock()
	defer fake.getAccountBalanceMutex.RUnlock()
	return len(fake.getAccountBalanceArgsForCall)
}

func (fake *ConsensusClient) GetAccountBalanceCalls(stub func(*hedera.AccountID) (hedera.Hbar, error)) {
	fake.getAccountBalanceMutex.Lock()
	defer fake.getAccountBalanceMutex.Unlock()
	fake.GetAccountBalanceStub = stub
}

func (fake *ConsensusClient) GetAccountBalanceArgsForCall(i int) *hedera.AccountID {
	fake.getAccountBalanceMutex.RLock()
	defer fake.getAccountBalanceMutex.RUnlock()
	argsForCall := fake.getAccountBalanceArgsForCall[i]
	return argsForCall.arg1
}

func (fake *ConsensusClient) GetAccountBalanceReturns(result1 hedera.Hbar, result2 error) {
	fake.getAccountBalanceMutex.Lock()
	defer fake.getAccountBalanceMutex.Unlock()
	fake.GetAccountBalanceStub = nil
	fake.getAccountBalanceReturns = struct {
		result1 hedera.Hbar
		result2 error
	}{result1, result2}
}

func (fake *ConsensusClient) GetAccountBalanceReturnsOnCall(i int, result1 hedera.Hbar, result2 error) {
	fake.getAccountBalanceMutex.Lock()
	defer fake.getAccountBalanceMutex.Unlock()
	fake.GetAccountBalanceStub = nil
	if fake.getAccountBalanceReturnsOnCall == nil {
		fake.getAccountBalanceReturnsOnCall = make(map[int]struct {
			result1 hedera.Hbar
			result2 error
		})
	}
	fake.getAccountBalanceReturnsOnCall[i] = struct {
		result1 hedera.Hbar
		result2 error
	}{result1, result2}
}

func (fake *ConsensusClient) GetConsensusTopicInfo(arg1 *hedera.ConsensusTopicID) (*hedera.ConsensusTopicInfo, error) {
	fake.getConsensusTopicInfoMutex.Lock()
	ret, specificReturn := fake.getConsensusTopicInfoReturnsOnCall[len(fake.getConsensusTopicInfoArgsForCall)]
	fake.getConsensusTopicInfoArgsForCall = append(fake.getConsensusTopicInfoArgsForCall, struct {
		arg1 *hedera.ConsensusTopicID
	}{arg1})
	fake.recordInvocation("GetConsensusTopicInfo", []interface{}{arg1})
	fake.getConsensusTopicInfoMutex.Unlock()
	if fake.GetConsensusTopicInfoStub != nil {
		return fake.GetConsensusTopicInfoStub(arg1)
	}
	if specificReturn {
		return ret.result1, ret.result2
	}
	fakeReturns := fake.getConsensusTopicInfoReturns
	return fakeReturns.result1, fakeReturns.result2
}

func (fake *ConsensusClient) GetConsensusTopicInfoCallCount() int {
	fake.getConsensusTopicInfoMutex.RLock()
	defer fake.getConsensusTopicInfoMutex.RUnlock()
	return len(fake.getConsensusTopicInfoArgsForCall)
}

func (fake *ConsensusClient) GetConsensusTopicInfoCalls(stub func(*hedera.ConsensusTopicID) (*hedera.ConsensusTopicInfo, error)) {
	fake.getConsensusTopicInfoMutex.Lock()
	defer fake.getConsensusTopicInfoMutex.Unlock()
	fake.GetConsensusTopicInfoStub = stub
}

func (fake *ConsensusClient) GetConsensusTopicInfoArgsForCall(i int) *hedera.ConsensusTopicID {
	fake.getConsensusTopicInfoMutex.RLock()
	defer fake.getConsensusTopicInfoMutex.RUnlock()
	argsForCall := fake.getConsensusTopicInfoArgsForCall[i]
	return argsForCall.arg1
}

func (fake *ConsensusClient) GetConsensusTopicInfoReturns(result1 *hedera.ConsensusTopicInfo, result2 error) {
	fake.getConsensusTopicInfoMutex.Lock()
	defer fake.getConsensusTopicInfoMutex.Unlock()
	fake.GetConsensusTopicInfoStub = nil
	fake.getConsensusTopicInfoReturns = struct {
		result1 *hedera.ConsensusTopicInfo
		result2 error
	}{result1, result2}
}

func (fake *ConsensusClient) GetConsensusTopicInfoReturnsOnCall(i int, result1 *hedera.ConsensusTopicInfo, result2 error) {
	fake.getConsensusTopicInfoMutex.Lock()
	defer fake.getConsensusTopicInfoMutex.Unlock()
	fake.GetConsensusTopicInfoStub = nil
	if fake.getConsensusTopicInfoReturnsOnCall == nil {
		fake.getConsensusTopicInfoReturnsOnCall = make(map[int]struct {
			result1 *hedera.ConsensusTopicInfo
			result2 error
		})
	}
	fake.getConsensusTopicInfoReturnsOnCall[i] = struct {
		result1 *hedera.ConsensusTopicInfo
		result2 error
	}{result1, result2}
}

func (fake *ConsensusClient) GetTransactionReceipt(arg1 *hedera.TransactionID) (*hedera.TransactionReceipt, error) {
	fake.getTransactionReceiptMutex.Lock()
	ret, specificReturn := fake.getTransactionReceiptReturnsOnCall[len(fake.getTransactionReceiptArgsForCall)]
	fake.getTransactionReceiptArgsForCall = append(fake.getTransactionReceiptArgsForCall, struct {
		arg1 *hedera.TransactionID
	}{arg1})
	fake.recordInvocation("GetTransactionReceipt", []interface{}{arg1})
	fake.getTransactionReceiptMutex.Unlock()
	if fake.GetTransactionReceiptStub != nil {
		return fake.GetTransactionReceiptStub(arg1)
	}
	if specificReturn {
		return ret.result1, ret.result2
	}
	fakeReturns := fake.getTransactionReceiptReturns
	return fakeReturns.result1, fakeReturns.result2
}

func (fake *ConsensusClient) GetTransactionReceiptCallCount() int {
	fake.getTransactionReceiptMutex.RLock()
	defer fake.getTransactionReceiptMutex.RUnlock()
	return len(fake.getTransactionReceiptArgsForCall)
}

func (fake *ConsensusClient) GetTransactionReceiptCalls(stub func(*hedera.TransactionID) (*hedera.TransactionReceipt, error)) {
	fake.getTransactionReceiptMutex.Lock()
	defer fake.getTransactionReceiptMutex.Unlock()
	fake.GetTransactionReceiptStub = stub
}

func (fake *ConsensusClient) GetTransactionReceiptArgsForCall(i int) *hedera.TransactionID {
	fake.getTransactionReceiptMutex.RLock()
	defer fake.getTransactionReceiptMutex.RUnlock()
	argsForCall := fake.getTransactionReceiptArgsForCall[i]
	return argsForCall.arg1
}

func (fake *ConsensusClient) GetTransactionReceiptReturns(result1 *hedera.TransactionReceipt, result2 error) {
	fake.getTransactionReceiptMutex.Lock()
	defer fake.getTransactionReceiptMutex.Unlock()
	fake.GetTransactionReceiptStub = nil
	fake.getTransactionReceiptReturns = struct {
		result1 *hedera.TransactionReceipt
		result2 error
	}{result1, result2}
}

func (fake *ConsensusClient) GetTransactionReceiptReturnsOnCall(i int, result1 *hedera.TransactionReceipt, result2 error) {
	fake.getTransactionReceiptMutex.Lock()
	defer fake.getTransactionReceiptMutex.Unlock()
	fake.GetTransactionReceiptStub = nil
	if fake.getTransactionReceiptReturnsOnCall == nil {
		fake.getTransactionReceiptReturnsOnCall = make(map[int]struct {
			result1 *hedera.TransactionReceipt
			result2 error
		})
	}
	fake.getTransactionReceiptReturnsOnCall[i] = struct {
		result1 *hedera.TransactionReceipt
		result2 error
	}{result1, result2}
}

func (fake *ConsensusClient) SubmitConsensusMessage(arg1 []byte, arg2 *hedera.ConsensusTopicID) (*hedera.TransactionID, error) {
	var arg1Copy []byte
	if arg1 != nil {
		arg1Copy = make([]byte, len(arg1))
		copy(arg1Copy, arg1)
	}
	fake.submitConsensusMessageMutex.Lock()
	ret, specificReturn := fake.submitConsensusMessageReturnsOnCall[len(fake.submitConsensusMessageArgsForCall)]
	fake.submitConsensusMessageArgsForCall = append(fake.submitConsensusMessageArgsForCall, struct {
		arg1 []byte
		arg2 *hedera.ConsensusTopicID
	}{arg1Copy, arg2})
	fake.recordInvocation("SubmitConsensusMessage", []interface{}{arg1Copy, arg2})
	fake.submitConsensusMessageMutex.Unlock()
	if fake.SubmitConsensusMessageStub != nil {
		return fake.SubmitConsensusMessageStub(arg1, arg2)
	}
	if specificReturn {
		return ret.result1, ret.result2
	}
	fakeReturns := fake.submitConsensusMessageReturns
	return fakeReturns.result1, fakeReturns.result2
}

func (fake *ConsensusClient) SubmitConsensusMessageCallCount() int {
	fake.submitConsensusMessageMutex.RLock()
	defer fake.submitConsensusMessageMutex.RUnlock()
	return len(fake.submitConsensusMessageArgsForCall)
}

func (fake *ConsensusClient) SubmitConsensusMessageCalls(stub func([]byte, *hedera.ConsensusTopicID) (*hedera.TransactionID, error)) {
	fake.submitConsensusMessageMutex.Lock()
	defer fake.submitConsensusMessageMutex.Unlock()
	fake.SubmitConsensusMessageStub = stub
}

func (fake *ConsensusClient) SubmitConsensusMessageArgsForCall(i int) ([]byte, *hedera.ConsensusTopicID) {
	fake.submitConsensusMessageMutex.RLock()
	defer fake.submitConsensusMessageMutex.RUnlock()
	argsForCall := fake.submitConsensusMessageArgsForCall[i]
	return argsForCall.arg1, argsForCall.arg2
}

func (fake *ConsensusClient) SubmitConsensusMessageReturns(result1 *hedera.TransactionID, result2 error) {
	fake.submitConsensusMessageMutex.Lock()
	defer fake.submitConsensusMessageMutex.Unlock()
	fake.SubmitConsensusMessageStub = nil
	fake.submitConsensusMessageReturns = struct {
		result1 *hedera.TransactionID
		result2 error
	}{result1, result2}
}

func (fake *ConsensusClient) SubmitConsensusMessageReturnsOnCall(i int, result1 *hedera.TransactionID, result2 error) {
	fake.submitConsensusMessageMutex.Lock()
	defer fake.submitConsensusMessageMutex.Unlock()
	fake.SubmitConsensusMessageStub = nil
	if fake.submitConsensusMessageReturnsOnCall == nil {
		fake.submitConsensusMessageReturnsOnCall = make(map[int]struct {
			result1 *hedera.TransactionID
			result2 error
		})
	}
	fake.submitConsensusMessageReturnsOnCall[i] = struct {
		result1 *hedera.TransactionID
		result2 error
	}{result1, result2}
}

func (fake *ConsensusClient) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.closeMutex.RLock()
	defer fake.closeMutex.RUnlock()
	fake.getAccountBalanceMutex.RLock()
	defer fake.getAccountBalanceMutex.RUnlock()
	fake.getConsensusTopicInfoMutex.RLock()
	defer fake.getConsensusTopicInfoMutex.RUnlock()
	fake.getTransactionReceiptMutex.RLock()
	defer fake.getTransactionReceiptMutex.RUnlock()
	fake.submitConsensusMessageMutex.RLock()
	defer fake.submitConsensusMessageMutex.RUnlock()
	copiedInvocations := map[string][][]interface{}{}
	for key, value := range fake.invocations {
		copiedInvocations[key] = value
	}
	return copiedInvocations
}

func (fake *ConsensusClient) recordInvocation(key string, args []interface{}) {
	fake.invocationsMutex.Lock()
	defer fake.invocationsMutex.Unlock()
	if fake.invocations == nil {
		fake.invocations = map[string][][]interface{}{}
	}
	if fake.invocations[key] == nil {
		fake.invocations[key] = [][]interface{}{}
	}
	fake.invocations[key] = append(fake.invocations[key], args)
}
