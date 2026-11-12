-- Stage 130.8 (72.D h / 84.6, user decision 2026-10-22: encrypt). A gift
-- card's number is a bearer credential. stored_value_instruments.card_number
-- now holds a keyed hash for lookups; card_enc the number encrypted under
-- STORED_VALUE_KEY (base64 of the keyring's versioned ciphertext), for an
-- administrator's reprint; card_last4 what screens print. Additive: the
-- server seals the existing cards when it starts (EncryptStoredValueCards),
-- since only it holds the key. Customer advances keep their CA number.
DO $mig$
DECLARE
  schema_rec RECORD;
BEGIN
  FOR schema_rec IN
    SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE 'tenant\_%' ESCAPE '\'
  LOOP
    CONTINUE WHEN to_regclass(format('%I.stored_value_instruments', schema_rec.schema_name)) IS NULL;
    EXECUTE format('ALTER TABLE %I.stored_value_instruments ADD COLUMN IF NOT EXISTS card_last4 TEXT', schema_rec.schema_name);
    EXECUTE format('ALTER TABLE %I.stored_value_instruments ADD COLUMN IF NOT EXISTS card_enc TEXT', schema_rec.schema_name);
  END LOOP;
END
$mig$;
