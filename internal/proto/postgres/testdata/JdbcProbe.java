import java.sql.*;
import java.util.Properties;

/** Optional local oracle; run through TestJDBC with POSTGRES_JDBC_JAR set. */
class JdbcProbe {
    public static void main(String[] args) throws Exception {
        Properties properties = new Properties();
        properties.setProperty("user", "u");
        properties.setProperty("password", "dummy-pass");
        properties.setProperty("connectTimeout", "5");
        properties.setProperty("socketTimeout", "5");
        try (Connection c = DriverManager.getConnection("jdbc:postgresql://" + args[0] + "/fixture", properties)) {
            try (Statement s = c.createStatement()) {
                s.execute("CREATE TABLE fixture (id INTEGER PRIMARY KEY, label VARCHAR(40))");
                s.execute("INSERT INTO fixture VALUES (1, 'network-ok')");
                try (ResultSet r = s.executeQuery("SELECT id,label FROM fixture")) {
                    if (!r.next() || r.getInt(1) != 1 || !r.getString(2).equals("network-ok")) throw new AssertionError("table round trip");
                }
                try (ResultSet r = s.executeQuery("SELECT '{\"ok\":true}'::jsonb AS payload, '00000000-0000-0000-0000-000000000001'::uuid AS id")) {
                    if (!r.next() || !r.getObject(1).toString().equals("{\"ok\":true}") || !r.getObject(2).toString().endsWith("000000000001")) throw new AssertionError("typed values");
                }
            }
            try (ResultSet columns = c.getMetaData().getColumns(null,null,"fixture","%")) {
                boolean label = false;
                while (columns.next()) if (columns.getString("COLUMN_NAME").equals("label")) label = true;
                if (!label) throw new AssertionError("JDBC columns metadata");
            }
            try (PreparedStatement p = c.prepareStatement("SELECT ?::int4 AS n, ?::text AS label, ?::bool AS flag")) {
                for (int i=0;i<8;i++) {
                    p.setInt(1,i);p.setString(2,"prepared value");p.setBoolean(3,true);
                    try (ResultSet r = p.executeQuery()) {
                        if (!r.next()) throw new AssertionError("missing prepared row " + i);
                        if (r.getInt(1)!=i || !r.getString(2).equals("prepared value") || !r.getBoolean(3)) throw new AssertionError("prepared/binary round trip " + i + ": " + r.getInt(1) + "/" + r.getString(2) + "/" + r.getString(3) + "/" + r.getBoolean(3));
                    }
                }
            }
        }
        System.out.println("PASS JDBC startup/auth, table/metadata, JSONB/UUID, prepared statements (8 executions)");
    }
}
