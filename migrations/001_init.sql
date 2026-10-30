CREATE TABLE IF NOT EXISTS customers (
    id         INT UNSIGNED NOT NULL AUTO_INCREMENT,
    name       VARCHAR(100) NOT NULL,
    created_at DATETIME(3)  NOT NULL,
    PRIMARY KEY (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS wallets (
    customer_id INT UNSIGNED NOT NULL,
    balance     BIGINT      NOT NULL DEFAULT 0,
    blocked     BIGINT      NOT NULL DEFAULT 0,
    updated_at  DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (customer_id),
    CONSTRAINT fk_wallet_customer FOREIGN KEY (customer_id) REFERENCES customers (id),
    CONSTRAINT chk_wallet_no_overdraft CHECK (blocked >= 0 AND balance >= blocked)
) ENGINE = InnoDB;

CREATE TABLE IF NOT EXISTS messages (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    customer_id   INT UNSIGNED NOT NULL,
    client_id     BINARY(16)   NOT NULL,
    lane          ENUM ('normal', 'express') NOT NULL,
    phone         VARCHAR(20)  NOT NULL,
    body          VARCHAR(160) NOT NULL,
    price         BIGINT       NOT NULL,
    status        ENUM ('PENDING', 'SENDING', 'SENT', 'FAILED', 'EXPIRED') NOT NULL,
    attempt       INT          NOT NULL DEFAULT 0,
    available_at  DATETIME(3)  NULL,
    expires_at    DATETIME(3)  NOT NULL,
    operator_ref  VARCHAR(64)  NULL,
    last_error    VARCHAR(255) NULL,
    created_at    DATETIME(3)  NOT NULL,
    updated_at    DATETIME(3)  NOT NULL,
    sent_at       DATETIME(3)  NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_client_id (customer_id, client_id),
    KEY idx_dispatch (customer_id, lane, available_at),
    KEY idx_report (customer_id, id),
    KEY idx_backlog (lane, available_at, customer_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
