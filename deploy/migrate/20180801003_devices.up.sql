
CREATE TABLE devices (
  user_id UUID not null,
  device_id UUID not null,
  created_at TIMESTAMP not null,
  updated_at TIMESTAMP not null,
  device_state SMALLINT not null,
  PRIMARY KEY (user_id, device_id)
);
