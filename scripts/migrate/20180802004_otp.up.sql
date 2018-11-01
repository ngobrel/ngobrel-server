
CREATE TABLE otp (
  otp_code BIGINT not null,
  expired_at TIMESTAMP not null,
  PRIMARY KEY (otp_code)
);
