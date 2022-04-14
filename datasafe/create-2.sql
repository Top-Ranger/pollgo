CREATE DATABASE pollgo;
CREATE TABLE pollgo.poll (name VARCHAR(600) NOT NULL, data LONGBLOB NOT NULL, creator TEXT, deleted BOOLEAN, PRIMARY KEY(name));
CREATE TABLE pollgo.result (id BIGINT UNSIGNED AUTO_INCREMENT, poll VARCHAR(600) NOT NULL, name MEDIUMTEXT NOT NULL, comment MEDIUMTEXT NOT NULL, results LONGBLOB NOT NULL, `change` TINYTEXT, PRIMARY KEY (id), FOREIGN KEY (poll) REFERENCES poll (name) ON DELETE CASCADE ON UPDATE RESTRICT);
CREATE INDEX rp ON pollgo.result (poll);
