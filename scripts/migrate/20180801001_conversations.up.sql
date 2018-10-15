
CREATE TABLE conversations (
  recipient_id UUID not null,
  message_id BIGINT not null,
  sender_id UUID not null,
  sender_device_id UUID not null,
  recipient_device_id UUID not null,
  message_timestamp TIMESTAMP not null,
  message_contents text,
  message_encrypted boolean,
  PRIMARY KEY (message_id, sender_id, recipient_device_id)
);

CREATE INDEX conversations_recipient_device_id on conversations(recipient_device_id);