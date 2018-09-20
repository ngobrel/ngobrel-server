
CREATE TABLE conversations_state (
  recipient_id UUID not null,
  message_id INT not null,
  sender_id UUID not null,
  created_at TIMESTAMP not null,
  updated_at TIMESTAMP not null,
  message_state SMALLINT not null,
  reception_state SMALLINT not null,
  PRIMARY KEY (message_id, sender_id, recipient_id)
);
