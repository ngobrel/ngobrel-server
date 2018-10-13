
CREATE TABLE devices (
  user_id UUID not null,
  device_id UUID not null,
  created_at TIMESTAMP not null,
  updated_at TIMESTAMP not null,
  device_state SMALLINT not null
);

CREATE UNIQUE INDEX devices_user_id_device_id on devices(user_id, device_id);
