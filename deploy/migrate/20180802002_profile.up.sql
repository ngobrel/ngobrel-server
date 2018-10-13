
CREATE TABLE profile  (
  user_id UUID not null,
  name text,
  phone_number text not null,
  user_name text,
  avatar bytea,
  created_at TIMESTAMP not null,
  updated_at TIMESTAMP not null,
  custom_data text,
  PRIMARY KEY (user_id)
);