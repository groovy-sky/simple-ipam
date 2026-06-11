CREATE TABLE IF NOT EXISTS spaces (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  name VARCHAR(255) NOT NULL,
  description TEXT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_spaces_name (name)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS blocks (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  space_id BIGINT UNSIGNED NOT NULL,
  family TINYINT NOT NULL,
  cidr VARCHAR(64) NOT NULL,
  prefix_len TINYINT NOT NULL,
  start_addr VARCHAR(64) NOT NULL,
  end_addr VARCHAR(64) NOT NULL,
  start_ipv4 INT UNSIGNED NULL,
  end_ipv4 INT UNSIGNED NULL,
  start_ipv6_hi BIGINT UNSIGNED NULL,
  start_ipv6_lo BIGINT UNSIGNED NULL,
  end_ipv6_hi BIGINT UNSIGNED NULL,
  end_ipv6_lo BIGINT UNSIGNED NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_block_space_start_len (space_id, family, start_addr, prefix_len),
  KEY idx_blocks_space_id (space_id),
  KEY idx_blocks_ipv4_bounds (space_id, family, start_ipv4, end_ipv4),
  KEY idx_blocks_ipv6_start (space_id, family, start_ipv6_hi, start_ipv6_lo),
  KEY idx_blocks_ipv6_end (space_id, family, end_ipv6_hi, end_ipv6_lo),
  CONSTRAINT fk_blocks_space FOREIGN KEY (space_id) REFERENCES spaces (id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS prefixes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  parent_id BIGINT UNSIGNED NULL,
  block_id BIGINT UNSIGNED NULL,
  family TINYINT NOT NULL,
  cidr VARCHAR(64) NOT NULL,
  prefix_len TINYINT NOT NULL,
  start_addr VARCHAR(64) NOT NULL,
  end_addr VARCHAR(64) NOT NULL,
  start_ipv4 INT UNSIGNED NULL,
  end_ipv4 INT UNSIGNED NULL,
  start_ipv6_hi BIGINT UNSIGNED NULL,
  start_ipv6_lo BIGINT UNSIGNED NULL,
  end_ipv6_hi BIGINT UNSIGNED NULL,
  end_ipv6_lo BIGINT UNSIGNED NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_prefix_family_start_len (family, start_addr, prefix_len),
  KEY idx_prefix_parent_id (parent_id),
  KEY idx_prefix_block_id (block_id),
  KEY idx_prefixes_ipv4_bounds (family, parent_id, start_ipv4, end_ipv4),
  KEY idx_prefixes_ipv6_start (family, parent_id, start_ipv6_hi, start_ipv6_lo),
  KEY idx_prefixes_ipv6_end (family, parent_id, end_ipv6_hi, end_ipv6_lo),
  CONSTRAINT fk_prefixes_parent FOREIGN KEY (parent_id) REFERENCES prefixes (id),
  CONSTRAINT fk_prefixes_block FOREIGN KEY (block_id) REFERENCES blocks (id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS ip_addresses (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  family TINYINT NOT NULL,
  address VARCHAR(64) NOT NULL,
  prefix_id BIGINT UNSIGNED NOT NULL,
  ip_ipv4 INT UNSIGNED NULL,
  ip_ipv6_hi BIGINT UNSIGNED NULL,
  ip_ipv6_lo BIGINT UNSIGNED NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'allocated',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_address_family (family, address),
  KEY idx_prefix_id (prefix_id),
  KEY idx_ip_prefix_numeric (prefix_id, ip_ipv4, ip_ipv6_hi, ip_ipv6_lo),
  CONSTRAINT fk_ip_addresses_prefix FOREIGN KEY (prefix_id) REFERENCES prefixes (id)
) ENGINE=InnoDB;
