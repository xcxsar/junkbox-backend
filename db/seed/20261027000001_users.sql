-- Seed de Usuarios del Equipo Junkbox
-- Hash bcrypt (coste 12) para 'Password123!': $2b$12$PEizbOXrjE8LHclD/RRJiuKQvZRqdY.iLxrYki9Z5lAJOWmlgAUfe

INSERT INTO users (id, username, email, hashed_password, display_name, avatar_url) VALUES
('b1000000-0000-0000-0000-000000000001', 'cesar_beltran', 'cesar@junkbox.local', '$2b$12$PEizbOXrjE8LHclD/RRJiuKQvZRqdY.iLxrYki9Z5lAJOWmlgAUfe', 'César David Beltrán Loredo', 'https://api.dicebear.com/7.x/bottts/svg?seed=cesar'),
('b2000000-0000-0000-0000-000000000002', 'carlos_andrade', 'carlos@junkbox.local', '$2b$12$PEizbOXrjE8LHclD/RRJiuKQvZRqdY.iLxrYki9Z5lAJOWmlgAUfe', 'Carlos Eduardo Andrade Gámez', 'https://api.dicebear.com/7.x/bottts/svg?seed=carlos'),
('b3000000-0000-0000-0000-000000000003', 'omar_delangel', 'omar@junkbox.local', '$2b$12$PEizbOXrjE8LHclD/RRJiuKQvZRqdY.iLxrYki9Z5lAJOWmlgAUfe', 'Omar Del Angel Villa', 'https://api.dicebear.com/7.x/bottts/svg?seed=omar'),
('b4000000-0000-0000-0000-000000000004', 'raymundo_sanchez', 'raymundo@junkbox.local', '$2b$12$PEizbOXrjE8LHclD/RRJiuKQvZRqdY.iLxrYki9Z5lAJOWmlgAUfe', 'José Raymundo Sánchez Alcalde', 'https://api.dicebear.com/7.x/bottts/svg?seed=raymundo')
ON CONFLICT (id) DO UPDATE SET
    username = EXCLUDED.username,
    email = EXCLUDED.email,
    hashed_password = EXCLUDED.hashed_password,
    display_name = EXCLUDED.display_name,
    avatar_url = EXCLUDED.avatar_url;
