package configs

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type Config struct {
	BlockSizeKb                 uint32 `json:"block_size_kb"`
	BlockCacheSizeKb            uint32 `json:"block_cache_size_kb"`
	CacheSizeKb                 uint32 `json:"cache_size_kb"`
	SummarySparsity             uint8  `json:"summary_sparsity"`
	WalSegmentSizeInBlocks      uint16 `json:"wal_segment_size_in_blocks"`
	MemtableMaxSizeKb           uint32 `json:"memtable_max_size_kb"`
	MemtableInstancesCount      uint8  `json:"memtable_instances_count"`
	TokenBucketCapacity         uint16 `json:"token_bucket_capacity"`
	TokenBucketRefillIntervalMs uint16 `json:"token_bucket_refill_interval_ms"`
}

var (
	ReadFileError  = errors.New("[CONFIG]: There was an issue reading the config file.")
	JSONParseError = errors.New("[CONFIG]: There was an issue parsing the JSON config values.")
)

func NewDefaultConfig() *Config {
	return &Config{
		BlockSizeKb:                 BLOCK_SIZE_KB,
		BlockCacheSizeKb:            BLOCK_CACHE_SIZE_KB,
		CacheSizeKb:                 CACHE_SIZE_KB,
		SummarySparsity:             SUMMARY_SPARSITY,
		WalSegmentSizeInBlocks:      WAL_SEGMENT_SIZE_IN_BLOCKS,
		MemtableMaxSizeKb:           MEMTABLE_MAX_SIZE_KB,
		MemtableInstancesCount:      MEMTABLE_INSTANCES_COUNT,
		TokenBucketCapacity:         TOKEN_BUCKET_CAPACITY,
		TokenBucketRefillIntervalMs: TOKEN_BUCKET_REFILL_INTERVAL_MS,
	}
}

func LoadConfig() (*Config, error) {
	config := NewDefaultConfig()

	configData, err := os.ReadFile("config.json")
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil // if the file doesnt even exist we'll return the default
		}

		return nil, fmt.Errorf("%w: %s", ReadFileError, err)
	}

	// if the file exists, but is completely empty, also return the default without errors
	if len(configData) == 0 {
		return config, nil
	}

	if err := json.Unmarshal(configData, config); err != nil {
		return nil, fmt.Errorf("%w: %s", JSONParseError, err)
	}

	return config, nil
}
