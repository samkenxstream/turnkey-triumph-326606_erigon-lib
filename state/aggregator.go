/*
   Copyright 2022 Erigon contributors

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package state

import (
	"fmt"

	"sync"

	"github.com/RoaringBitmap/roaring/roaring64"
	"github.com/ledgerwatch/erigon-lib/kv"
)

// Reconstruction of the aggregator in another package, `aggregator`

type Aggregator struct {
	aggregationStep uint64
	accounts        *Domain
	storage         *Domain
	code            *Domain
	logAddrs        *InvertedIndex
	logTopics       *InvertedIndex
	tracesFrom      *InvertedIndex
	tracesTo        *InvertedIndex
	txNum           uint64
	rwTx            kv.RwTx
	keyBuf          []byte
}

func NewAggregator(
	dir string,
	aggregationStep uint64,
) (*Aggregator, error) {
	a := &Aggregator{
		aggregationStep: aggregationStep,
	}
	closeAgg := true
	defer func() {
		if closeAgg {
			if a.accounts != nil {
				a.accounts.Close()
			}
			if a.storage != nil {
				a.storage.Close()
			}
			if a.code != nil {
				a.code.Close()
			}
			if a.logAddrs != nil {
				a.logAddrs.Close()
			}
			if a.logTopics != nil {
				a.logTopics.Close()
			}
			if a.tracesFrom != nil {
				a.tracesFrom.Close()
			}
			if a.tracesTo != nil {
				a.tracesTo.Close()
			}
		}
	}()
	var err error
	if a.accounts, err = NewDomain(dir, aggregationStep, "accounts", kv.AccountKeys, kv.AccountVals, kv.AccountHistoryKeys, kv.AccountHistoryVals, kv.AccountSettings, kv.AccountIdx, 0 /* prefixLen */, false /* compressVals */); err != nil {
		return nil, err
	}
	if a.storage, err = NewDomain(dir, aggregationStep, "storage", kv.StorageKeys, kv.StorageVals, kv.StorageHistoryKeys, kv.StorageHistoryVals, kv.StorageSettings, kv.StorageIdx, 20 /* prefixLen */, false /* compressVals */); err != nil {
		return nil, err
	}
	if a.code, err = NewDomain(dir, aggregationStep, "code", kv.CodeKeys, kv.CodeVals, kv.CodeHistoryKeys, kv.CodeHistoryVals, kv.CodeSettings, kv.CodeIdx, 0 /* prefixLen */, true /* compressVals */); err != nil {
		return nil, err
	}
	if a.logAddrs, err = NewInvertedIndex(dir, aggregationStep, "logaddrs", kv.LogAddressKeys, kv.LogAddressIdx); err != nil {
		return nil, err
	}
	if a.logTopics, err = NewInvertedIndex(dir, aggregationStep, "logtopics", kv.LogTopicsKeys, kv.LogTopicsIdx); err != nil {
		return nil, err
	}
	if a.tracesFrom, err = NewInvertedIndex(dir, aggregationStep, "tracesfrom", kv.TracesFromKeys, kv.TracesFromIdx); err != nil {
		return nil, err
	}
	if a.tracesTo, err = NewInvertedIndex(dir, aggregationStep, "tracesto", kv.TracesToKeys, kv.TracesToIdx); err != nil {
		return nil, err
	}
	closeAgg = false
	return a, nil
}

func (a *Aggregator) GetAndResetStats() DomainStats {
	stats := DomainStats{}
	stats.Accumulate(a.accounts.GetAndResetStats())
	stats.Accumulate(a.storage.GetAndResetStats())
	stats.Accumulate(a.code.GetAndResetStats())
	return stats
}

func (a *Aggregator) Close() {
	if a.accounts != nil {
		a.accounts.Close()
	}
	if a.storage != nil {
		a.storage.Close()
	}
	if a.code != nil {
		a.code.Close()
	}
	if a.logAddrs != nil {
		a.logAddrs.Close()
	}
	if a.logTopics != nil {
		a.logTopics.Close()
	}
	if a.tracesFrom != nil {
		a.tracesFrom.Close()
	}
	if a.tracesTo != nil {
		a.tracesTo.Close()
	}
}

func (a *Aggregator) SetTx(tx kv.RwTx) {
	a.rwTx = tx
	a.accounts.SetTx(tx)
	a.storage.SetTx(tx)
	a.code.SetTx(tx)
	a.logAddrs.SetTx(tx)
	a.logTopics.SetTx(tx)
	a.tracesFrom.SetTx(tx)
	a.tracesTo.SetTx(tx)
}

func (a *Aggregator) SetTxNum(txNum uint64) {
	a.txNum = txNum
	a.accounts.SetTxNum(txNum)
	a.storage.SetTxNum(txNum)
	a.code.SetTxNum(txNum)
	a.logAddrs.SetTxNum(txNum)
	a.logTopics.SetTxNum(txNum)
	a.tracesFrom.SetTxNum(txNum)
	a.tracesTo.SetTxNum(txNum)
}

type AggCollation struct {
	accounts   Collation
	storage    Collation
	code       Collation
	logAddrs   map[string]*roaring64.Bitmap
	logTopics  map[string]*roaring64.Bitmap
	tracesFrom map[string]*roaring64.Bitmap
	tracesTo   map[string]*roaring64.Bitmap
}

func (c AggCollation) Close() {
	c.accounts.Close()
	c.storage.Close()
	c.code.Close()
}

func (a *Aggregator) collate(step uint64, txFrom, txTo uint64, roTx kv.Tx) (AggCollation, error) {
	var ac AggCollation
	var err error
	closeColl := true
	defer func() {
		if closeColl {
			ac.Close()
		}
	}()
	if ac.accounts, err = a.accounts.collate(step, txFrom, txTo, roTx); err != nil {
		return AggCollation{}, err
	}
	if ac.storage, err = a.storage.collate(step, txFrom, txTo, roTx); err != nil {
		return AggCollation{}, err
	}
	if ac.code, err = a.code.collate(step, txFrom, txTo, roTx); err != nil {
		return AggCollation{}, err
	}
	if ac.logAddrs, err = a.logAddrs.collate(txFrom, txTo, roTx); err != nil {
		return AggCollation{}, err
	}
	if ac.logTopics, err = a.logTopics.collate(txFrom, txTo, roTx); err != nil {
		return AggCollation{}, err
	}
	if ac.tracesFrom, err = a.tracesFrom.collate(txFrom, txTo, roTx); err != nil {
		return AggCollation{}, err
	}
	if ac.tracesTo, err = a.tracesTo.collate(txFrom, txTo, roTx); err != nil {
		return AggCollation{}, err
	}
	closeColl = false
	return ac, nil
}

type AggStaticFiles struct {
	accounts   StaticFiles
	storage    StaticFiles
	code       StaticFiles
	logAddrs   InvertedFiles
	logTopics  InvertedFiles
	tracesFrom InvertedFiles
	tracesTo   InvertedFiles
}

func (sf AggStaticFiles) Close() {
	sf.accounts.Close()
	sf.storage.Close()
	sf.code.Close()
	sf.logAddrs.Close()
	sf.logTopics.Close()
	sf.tracesFrom.Close()
	sf.tracesTo.Close()
}

func (a *Aggregator) buildFiles(step uint64, collation AggCollation) (AggStaticFiles, error) {
	var sf AggStaticFiles
	closeFiles := true
	defer func() {
		if closeFiles {
			sf.Close()
		}
	}()
	var wg sync.WaitGroup
	wg.Add(7)
	errCh := make(chan error, 7)
	go func() {
		defer wg.Done()
		var err error
		if sf.accounts, err = a.accounts.buildFiles(step, collation.accounts); err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if sf.storage, err = a.storage.buildFiles(step, collation.storage); err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if sf.code, err = a.code.buildFiles(step, collation.code); err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if sf.logAddrs, err = a.logAddrs.buildFiles(step, collation.logAddrs); err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if sf.logTopics, err = a.logTopics.buildFiles(step, collation.logTopics); err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if sf.tracesFrom, err = a.tracesFrom.buildFiles(step, collation.tracesFrom); err != nil {
			errCh <- err
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if sf.tracesTo, err = a.tracesTo.buildFiles(step, collation.tracesTo); err != nil {
			errCh <- err
		}
	}()
	go func() {
		wg.Wait()
		close(errCh)
	}()
	var lastError error
	for err := range errCh {
		lastError = err
	}
	if lastError == nil {
		closeFiles = false
	}
	return sf, lastError
}

func (a *Aggregator) integrateFiles(sf AggStaticFiles, txNumFrom, txNumTo uint64) {
	a.accounts.integrateFiles(sf.accounts, txNumFrom, txNumTo)
	a.storage.integrateFiles(sf.storage, txNumFrom, txNumTo)
	a.code.integrateFiles(sf.code, txNumFrom, txNumTo)
	a.logAddrs.integrateFiles(sf.logAddrs, txNumFrom, txNumTo)
	a.logTopics.integrateFiles(sf.logTopics, txNumFrom, txNumTo)
	a.tracesFrom.integrateFiles(sf.tracesFrom, txNumFrom, txNumTo)
	a.tracesTo.integrateFiles(sf.tracesTo, txNumFrom, txNumTo)
}

func (a *Aggregator) prune(step uint64, txFrom, txTo uint64) error {
	if err := a.accounts.prune(step, txFrom, txTo); err != nil {
		return err
	}
	if err := a.storage.prune(step, txFrom, txTo); err != nil {
		return err
	}
	if err := a.code.prune(step, txFrom, txTo); err != nil {
		return err
	}
	if err := a.logAddrs.prune(txFrom, txTo); err != nil {
		return err
	}
	if err := a.logTopics.prune(txFrom, txTo); err != nil {
		return err
	}
	if err := a.tracesFrom.prune(txFrom, txTo); err != nil {
		return err
	}
	if err := a.tracesTo.prune(txFrom, txTo); err != nil {
		return err
	}
	return nil
}

func (a *Aggregator) EndTxNumMinimax() uint64 {
	min := a.accounts.endTxNumMinimax()
	if txNum := a.storage.endTxNumMinimax(); txNum < min {
		min = txNum
	}
	if txNum := a.code.endTxNumMinimax(); txNum < min {
		min = txNum
	}
	if txNum := a.logAddrs.endTxNumMinimax(); txNum < min {
		min = txNum
	}
	if txNum := a.logTopics.endTxNumMinimax(); txNum < min {
		min = txNum
	}
	if txNum := a.tracesFrom.endTxNumMinimax(); txNum < min {
		min = txNum
	}
	if txNum := a.tracesTo.endTxNumMinimax(); txNum < min {
		min = txNum
	}
	return min
}

type Ranges struct {
	accounts                                 DomainRanges
	storage                                  DomainRanges
	code                                     DomainRanges
	logAddrsStartTxNum, logAddrsEndTxNum     uint64
	logAddrs                                 bool
	logTopicsStartTxNum, logTopicsEndTxNum   uint64
	logTopics                                bool
	tracesFromStartTxNum, tracesFromEndTxNum uint64
	tracesFrom                               bool
	tracesToStartTxNum, tracesToEndTxNum     uint64
	tracesTo                                 bool
}

func (r Ranges) any() bool {
	return r.accounts.any() || r.storage.any() || r.code.any() || r.logAddrs || r.logTopics || r.tracesFrom || r.tracesTo
}

func (a *Aggregator) findMergeRange(maxEndTxNum, maxSpan uint64) Ranges {
	var r Ranges
	r.accounts = a.accounts.findMergeRange(maxEndTxNum, maxSpan)
	r.storage = a.storage.findMergeRange(maxEndTxNum, maxSpan)
	r.code = a.code.findMergeRange(maxEndTxNum, maxSpan)
	r.logAddrs, r.logAddrsStartTxNum, r.logAddrsEndTxNum = a.logAddrs.findMergeRange(maxEndTxNum, maxSpan)
	r.logTopics, r.logTopicsStartTxNum, r.logTopicsEndTxNum = a.logTopics.findMergeRange(maxEndTxNum, maxSpan)
	r.tracesFrom, r.tracesFromStartTxNum, r.tracesFromEndTxNum = a.tracesFrom.findMergeRange(maxEndTxNum, maxSpan)
	r.tracesTo, r.tracesToStartTxNum, r.tracesToEndTxNum = a.tracesTo.findMergeRange(maxEndTxNum, maxSpan)
	fmt.Printf("findMergeRange(%d, %d)=%+v\n", maxEndTxNum, maxSpan, r)
	return r
}

type SelectedStaticFiles struct {
	accounts    [][NumberOfTypes]*filesItem
	accountsI   int
	storage     [][NumberOfTypes]*filesItem
	storageI    int
	code        [][NumberOfTypes]*filesItem
	codeI       int
	logAddrs    []*filesItem
	logAddrsI   int
	logTopics   []*filesItem
	logTopicsI  int
	tracesFrom  []*filesItem
	tracesFromI int
	tracesTo    []*filesItem
	tracesToI   int
}

func (sf SelectedStaticFiles) Close() {
	for fType := FileType(0); fType < NumberOfTypes; fType++ {
		for _, group := range [][][NumberOfTypes]*filesItem{sf.accounts, sf.storage, sf.code} {
			for _, items := range group {
				if items[fType] != nil {
					if items[fType].decompressor != nil {
						items[fType].decompressor.Close()
					}
					if items[fType].decompressor != nil {
						items[fType].index.Close()
					}
				}
			}
		}
	}
	for _, group := range [][]*filesItem{sf.logAddrs, sf.logTopics, sf.tracesFrom, sf.tracesTo} {
		for _, item := range group {
			if item != nil {
				if item.decompressor != nil {
					item.decompressor.Close()
				}
				if item.decompressor != nil {
					item.index.Close()
				}
			}
		}
	}
}

func (a *Aggregator) staticFilesInRange(r Ranges) SelectedStaticFiles {
	var sf SelectedStaticFiles
	if r.accounts.any() {
		sf.accounts, sf.accountsI = a.accounts.staticFilesInRange(r.accounts)
	}
	if r.storage.any() {
		sf.storage, sf.storageI = a.storage.staticFilesInRange(r.storage)
	}
	if r.code.any() {
		sf.code, sf.codeI = a.code.staticFilesInRange(r.code)
	}
	if r.logAddrs {
		sf.logAddrs, sf.logAddrsI = a.logAddrs.staticFilesInRange(r.logAddrsStartTxNum, r.logAddrsEndTxNum)
	}
	if r.logTopics {
		sf.logTopics, sf.logTopicsI = a.logTopics.staticFilesInRange(r.logTopicsStartTxNum, r.logTopicsEndTxNum)
	}
	if r.tracesFrom {
		sf.tracesFrom, sf.tracesFromI = a.tracesFrom.staticFilesInRange(r.tracesFromStartTxNum, r.tracesFromEndTxNum)
	}
	if r.tracesTo {
		sf.tracesTo, sf.tracesToI = a.tracesTo.staticFilesInRange(r.tracesToStartTxNum, r.tracesToEndTxNum)
	}
	return sf
}

type MergedFiles struct {
	accounts   [NumberOfTypes]*filesItem
	storage    [NumberOfTypes]*filesItem
	code       [NumberOfTypes]*filesItem
	logAddrs   *filesItem
	logTopics  *filesItem
	tracesFrom *filesItem
	tracesTo   *filesItem
}

func (mf MergedFiles) Close() {
	for fType := FileType(0); fType < NumberOfTypes; fType++ {
		for _, items := range [][NumberOfTypes]*filesItem{mf.accounts, mf.storage, mf.code} {
			if items[fType] != nil {
				if items[fType].decompressor != nil {
					items[fType].decompressor.Close()
				}
				if items[fType].decompressor != nil {
					items[fType].index.Close()
				}
			}
		}
	}
	for _, item := range []*filesItem{mf.logAddrs, mf.logTopics, mf.tracesFrom, mf.tracesTo} {
		if item != nil {
			if item.decompressor != nil {
				item.decompressor.Close()
			}
			if item.decompressor != nil {
				item.index.Close()
			}
		}
	}
}

func (a *Aggregator) mergeFiles(files SelectedStaticFiles, r Ranges, maxSpan uint64) (MergedFiles, error) {
	var mf MergedFiles
	closeFiles := true
	defer func() {
		if closeFiles {
			mf.Close()
		}
	}()
	var wg sync.WaitGroup
	wg.Add(7)
	errCh := make(chan error, 7)
	go func() {
		defer wg.Done()
		var err error
		if r.accounts.any() {
			if mf.accounts, err = a.accounts.mergeFiles(files.accounts, r.accounts, maxSpan); err != nil {
				errCh <- err
			}
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if r.storage.any() {
			if mf.storage, err = a.storage.mergeFiles(files.storage, r.storage, maxSpan); err != nil {
				errCh <- err
			}
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if r.code.any() {
			if mf.code, err = a.code.mergeFiles(files.code, r.code, maxSpan); err != nil {
				errCh <- err
			}
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if r.logAddrs {
			if mf.logAddrs, err = a.logAddrs.mergeFiles(files.logAddrs, r.logAddrsStartTxNum, r.logAddrsEndTxNum, maxSpan); err != nil {
				errCh <- err
			}
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if r.logTopics {
			if mf.logTopics, err = a.logTopics.mergeFiles(files.logTopics, r.logTopicsStartTxNum, r.logTopicsEndTxNum, maxSpan); err != nil {
				errCh <- err
			}
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if r.tracesFrom {
			if mf.tracesFrom, err = a.tracesFrom.mergeFiles(files.tracesFrom, r.tracesFromStartTxNum, r.tracesFromEndTxNum, maxSpan); err != nil {
				errCh <- err
			}
		}
	}()
	go func() {
		defer wg.Done()
		var err error
		if r.tracesTo {
			if mf.tracesTo, err = a.tracesTo.mergeFiles(files.tracesTo, r.tracesToStartTxNum, r.tracesToEndTxNum, maxSpan); err != nil {
				errCh <- err
			}
		}
	}()
	go func() {
		wg.Wait()
		close(errCh)
	}()
	var lastError error
	for err := range errCh {
		lastError = err
	}
	if lastError == nil {
		closeFiles = false
	}
	return mf, lastError
}

func (a *Aggregator) integrateMergedFiles(outs SelectedStaticFiles, in MergedFiles) {
	a.accounts.integrateMergedFiles(outs.accounts, in.accounts)
	a.storage.integrateMergedFiles(outs.storage, in.storage)
	a.code.integrateMergedFiles(outs.code, in.code)
	a.logAddrs.integrateMergedFiles(outs.logAddrs, in.logAddrs)
	a.logTopics.integrateMergedFiles(outs.logTopics, in.logTopics)
	a.tracesFrom.integrateMergedFiles(outs.tracesFrom, in.tracesFrom)
	a.tracesTo.integrateMergedFiles(outs.tracesTo, in.tracesTo)
}

func (a *Aggregator) deleteFiles(outs SelectedStaticFiles) error {
	if err := a.accounts.deleteFiles(outs.accounts); err != nil {
		return err
	}
	if err := a.storage.deleteFiles(outs.storage); err != nil {
		return err
	}
	if err := a.code.deleteFiles(outs.code); err != nil {
		return err
	}
	if err := a.logAddrs.deleteFiles(outs.logAddrs); err != nil {
		return err
	}
	if err := a.logTopics.deleteFiles(outs.logTopics); err != nil {
		return err
	}
	if err := a.tracesFrom.deleteFiles(outs.tracesFrom); err != nil {
		return err
	}
	if err := a.tracesTo.deleteFiles(outs.tracesTo); err != nil {
		return err
	}
	return nil
}

func (a *Aggregator) ReadAccountData(addr []byte, roTx kv.Tx) ([]byte, error) {
	return a.accounts.Get(addr, roTx)
}

func (a *Aggregator) ReadAccountDataBeforeTxNum(addr []byte, txNum uint64, roTx kv.Tx) ([]byte, error) {
	return a.accounts.GetBeforeTxNum(addr, txNum, roTx)
}

func (a *Aggregator) ReadAccountStorage(addr []byte, loc []byte, roTx kv.Tx) ([]byte, error) {
	if cap(a.keyBuf) < len(addr)+len(loc) {
		a.keyBuf = make([]byte, len(addr)+len(loc))
	} else if len(a.keyBuf) != len(addr)+len(loc) {
		a.keyBuf = a.keyBuf[:len(addr)+len(loc)]
	}
	copy(a.keyBuf, addr)
	copy(a.keyBuf[len(addr):], loc)
	return a.storage.Get(a.keyBuf, roTx)
}

func (a *Aggregator) ReadAccountStorageBeforeTxNum(addr []byte, loc []byte, txNum uint64, roTx kv.Tx) ([]byte, error) {
	if cap(a.keyBuf) < len(addr)+len(loc) {
		a.keyBuf = make([]byte, len(addr)+len(loc))
	} else if len(a.keyBuf) != len(addr)+len(loc) {
		a.keyBuf = a.keyBuf[:len(addr)+len(loc)]
	}
	copy(a.keyBuf, addr)
	copy(a.keyBuf[len(addr):], loc)
	return a.storage.GetBeforeTxNum(a.keyBuf, txNum, roTx)
}

func (a *Aggregator) ReadAccountCode(addr []byte, roTx kv.Tx) ([]byte, error) {
	return a.code.Get(addr, roTx)
}

func (a *Aggregator) ReadAccountCodeBeforeTxNum(addr []byte, txNum uint64, roTx kv.Tx) ([]byte, error) {
	return a.code.GetBeforeTxNum(addr, txNum, roTx)
}

func (a *Aggregator) ReadAccountCodeSize(addr []byte, roTx kv.Tx) (int, error) {
	code, err := a.code.Get(addr, roTx)
	if err != nil {
		return 0, err
	}
	return len(code), nil
}

func (a *Aggregator) ReadAccountCodeSizeBeforeTxNum(addr []byte, txNum uint64, roTx kv.Tx) (int, error) {
	code, err := a.code.GetBeforeTxNum(addr, txNum, roTx)
	if err != nil {
		return 0, err
	}
	return len(code), nil
}

func (a *Aggregator) ReadyToFinishTx() bool {
	return (a.txNum+1)%a.aggregationStep == 0
}

func (a *Aggregator) FinishTx() error {
	if (a.txNum+1)%a.aggregationStep != 0 {
		return nil
	}
	closeAll := true
	step := a.txNum / a.aggregationStep
	if step == 0 {
		return nil
	}
	step-- // Leave one step worth in the DB
	collation, err := a.collate(step, step*a.aggregationStep, (step+1)*a.aggregationStep, a.rwTx)
	if err != nil {
		return err
	}
	defer func() {
		if closeAll {
			collation.Close()
		}
	}()
	sf, err := a.buildFiles(step, collation)
	if err != nil {
		return err
	}
	defer func() {
		if closeAll {
			sf.Close()
		}
	}()
	a.integrateFiles(sf, step*a.aggregationStep, (step+1)*a.aggregationStep)
	if err = a.prune(step, step*a.aggregationStep, (step+1)*a.aggregationStep); err != nil {
		return err
	}
	maxEndTxNum := a.EndTxNumMinimax()
	maxSpan := uint64(32) * a.aggregationStep
	for r := a.findMergeRange(maxEndTxNum, maxSpan); r.any(); r = a.findMergeRange(maxEndTxNum, maxSpan) {
		outs := a.staticFilesInRange(r)
		defer func() {
			if closeAll {
				outs.Close()
			}
		}()
		in, err := a.mergeFiles(outs, r, maxSpan)
		if err != nil {
			return err
		}
		defer func() {
			if closeAll {
				in.Close()
			}
		}()
		a.integrateMergedFiles(outs, in)
		if err = a.deleteFiles(outs); err != nil {
			return err
		}
	}
	closeAll = false
	return nil
}

func (a *Aggregator) UpdateAccountData(addr []byte, account []byte) error {
	return a.accounts.Put(addr, account)
}

func (a *Aggregator) UpdateAccountCode(addr []byte, code []byte) error {
	if len(code) == 0 {
		return a.code.Delete(addr)
	}
	return a.code.Put(addr, code)
}

func (a *Aggregator) DeleteAccount(addr []byte) error {
	if err := a.accounts.Delete(addr); err != nil {
		return err
	}
	if err := a.code.Delete(addr); err != nil {
		return err
	}
	var e error
	if err := a.storage.IteratePrefix(addr, func(k, _ []byte) {
		if e == nil {
			e = a.storage.Delete(k)
		}
	}); err != nil {
		return err
	}
	return e
}

func (a *Aggregator) WriteAccountStorage(addr, loc []byte, value []byte) error {
	if cap(a.keyBuf) < len(addr)+len(loc) {
		a.keyBuf = make([]byte, len(addr)+len(loc))
	} else if len(a.keyBuf) != len(addr)+len(loc) {
		a.keyBuf = a.keyBuf[:len(addr)+len(loc)]
	}
	copy(a.keyBuf, addr)
	copy(a.keyBuf[len(addr):], loc)
	if len(value) == 0 {
		return a.storage.Delete(a.keyBuf)
	}
	return a.storage.Put(a.keyBuf, value)
}

func (a *Aggregator) AddTraceFrom(addr []byte) error {
	return a.tracesFrom.Add(addr)
}

func (a *Aggregator) AddTraceTo(addr []byte) error {
	return a.tracesTo.Add(addr)
}

func (a *Aggregator) AddLogAddr(addr []byte) error {
	return a.logAddrs.Add(addr)
}

func (a *Aggregator) AddLogTopic(topic []byte) error {
	return a.logTopics.Add(topic)
}

func (a *Aggregator) LogAddrIterator(addr []byte, startTxNum, endTxNum uint64, roTx kv.Tx) InvertedIterator {
	return a.logAddrs.IterateRange(addr, startTxNum, endTxNum, roTx)
}

func (a *Aggregator) LogTopicIterator(topic []byte, startTxNum, endTxNum uint64, roTx kv.Tx) InvertedIterator {
	return a.logTopics.IterateRange(topic, startTxNum, endTxNum, roTx)
}

func (a *Aggregator) TraceFromIterator(addr []byte, startTxNum, endTxNum uint64, roTx kv.Tx) InvertedIterator {
	return a.tracesFrom.IterateRange(addr, startTxNum, endTxNum, roTx)
}

func (a *Aggregator) TraceToIterator(addr []byte, startTxNum, endTxNum uint64, roTx kv.Tx) InvertedIterator {
	return a.tracesTo.IterateRange(addr, startTxNum, endTxNum, roTx)
}

type FilesStats struct {
}

func (a *Aggregator) Stats() FilesStats {
	var fs FilesStats
	return fs
}

type AggregatorContext struct {
	a        *Aggregator
	accounts *DomainContext
	storage  *DomainContext
	code     *DomainContext
	keyBuf   []byte
}

func (a *Aggregator) MakeContext() *AggregatorContext {
	return &AggregatorContext{
		a:        a,
		accounts: a.accounts.MakeContext(),
		storage:  a.storage.MakeContext(),
		code:     a.code.MakeContext(),
	}
}

func (ac *AggregatorContext) IterateAccountsReconTxs(fromKey, toKey []byte, txNum uint64) *ScanIterator {
	return ac.accounts.iterateReconTxs(fromKey, toKey, txNum)
}

func (ac *AggregatorContext) IterateStorageReconTxs(fromKey, toKey []byte, txNum uint64) *ScanIterator {
	return ac.storage.iterateReconTxs(fromKey, toKey, txNum)
}

func (ac *AggregatorContext) IterateCodeReconTxs(fromKey, toKey []byte, txNum uint64) *ScanIterator {
	return ac.code.iterateReconTxs(fromKey, toKey, txNum)
}

func (ac *AggregatorContext) ReadAccountDataNoState(addr []byte, txNum uint64) ([]byte, bool, uint64, error) {
	return ac.accounts.GetNoState(addr, txNum)
}

func (ac *AggregatorContext) ReadAccountStorageNoState(addr []byte, loc []byte, txNum uint64) ([]byte, bool, uint64, error) {
	if cap(ac.keyBuf) < len(addr)+len(loc) {
		ac.keyBuf = make([]byte, len(addr)+len(loc))
	} else if len(ac.keyBuf) != len(addr)+len(loc) {
		ac.keyBuf = ac.keyBuf[:len(addr)+len(loc)]
	}
	copy(ac.keyBuf, addr)
	copy(ac.keyBuf[len(addr):], loc)
	return ac.storage.GetNoState(ac.keyBuf, txNum)
}

func (ac *AggregatorContext) ReadAccountCodeNoState(addr []byte, txNum uint64) ([]byte, bool, uint64, error) {
	return ac.code.GetNoState(addr, txNum)
}

func (ac *AggregatorContext) ReadAccountCodeSizeNoState(addr []byte, txNum uint64) (int, bool, uint64, error) {
	code, noState, stateTxNum, err := ac.code.GetNoState(addr, txNum)
	if err != nil {
		return 0, false, 0, err
	}
	return len(code), noState, stateTxNum, nil
}

func (ac *AggregatorContext) MaxAccountsTxNum(addr []byte) (bool, uint64) {
	return ac.accounts.MaxTxNum(addr)
}

func (ac *AggregatorContext) MaxStorageTxNum(addr []byte, loc []byte) (bool, uint64) {
	if cap(ac.keyBuf) < len(addr)+len(loc) {
		ac.keyBuf = make([]byte, len(addr)+len(loc))
	} else if len(ac.keyBuf) != len(addr)+len(loc) {
		ac.keyBuf = ac.keyBuf[:len(addr)+len(loc)]
	}
	copy(ac.keyBuf, addr)
	copy(ac.keyBuf[len(addr):], loc)
	return ac.storage.MaxTxNum(ac.keyBuf)
}

func (ac *AggregatorContext) MaxCodeTxNum(addr []byte) (bool, uint64) {
	return ac.code.MaxTxNum(addr)
}

func (ac *AggregatorContext) IterateAccountsHistory(fromKey, toKey []byte, txNum uint64) *HistoryIterator {
	return ac.accounts.iterateHistoryBeforeTxNum(fromKey, toKey, txNum)
}

func (ac *AggregatorContext) IterateStorageHistory(fromKey, toKey []byte, txNum uint64) *HistoryIterator {
	return ac.storage.iterateHistoryBeforeTxNum(fromKey, toKey, txNum)
}

func (ac *AggregatorContext) IterateCodeHistory(fromKey, toKey []byte, txNum uint64) *HistoryIterator {
	return ac.code.iterateHistoryBeforeTxNum(fromKey, toKey, txNum)
}
