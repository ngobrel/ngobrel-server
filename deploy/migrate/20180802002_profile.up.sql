
CREATE TABLE profile  (
  user_id UUID not null,
  name text,
  phone_number text not null,
  userName text,
  avatar bytea,
  created_at TIMESTAMP not null,
  updated_at TIMESTAMP not null,
  PRIMARY KEY (user_id)
);