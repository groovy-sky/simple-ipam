CREATE TABLE IF NOT EXISTS prefixes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  family TINYINT NOT NULL,
  cidr VARCHAR(64) NOT NULL,
  prefix_len TINYINT NOT NULL,
  start_addr VARCHAR(64) NOT NULL,
  end_addr VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_prefix_family_start_len (family, start_addr, prefix_len)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS ip_addresses (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  family TINYINT NOT NULL,
  address VARCHAR(64) NOT NULL,
  prefix_id BIGINT UNSIGNED NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'allocated',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_address_family (family, address),
  KEY idx_prefix_id (prefix_id),
  CONSTRAINT fk_ip_addresses_prefix FOREIGN KEY (prefix_id) REFERENCES prefixes (id)
) ENGINE=InnoDB;
