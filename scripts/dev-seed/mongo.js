// Sample data for manually exercising the desktop app's MongoDB surfaces
// (Browser, Pipeline aggregation builder, Vector Compare via the
// embedding field, Geo Viewer via the location field).
//
// Usage:
//   mongosh "mongodb://localhost:27017/mongobak_test" scripts/dev-seed/mongo.js
//   mongobak connection add mongo-dev --uri "mongodb://localhost:27017"

db = db.getSiblingDB("mongobak_test");

db.users.drop();
db.punches.drop();

db.users.insertMany([
  {
    _id: ObjectId(),
    email: "ada@example.com",
    fullName: "Ada Lovelace",
    plan: "pro",
    // A toy 8-dim "face embedding" — paste one of these into Vector
    // Compare (or right-click the cell and "Send to Vector Compare") to
    // try cosine/Euclidean distance against another row's.
    faceEmbedding: [0.12, -0.34, 0.98, 0.05, -0.22, 0.61, -0.09, 0.44],
  },
  {
    _id: ObjectId(),
    email: "grace@example.com",
    fullName: "Grace Hopper",
    plan: "free",
    faceEmbedding: [0.15, -0.3, 0.95, 0.02, -0.19, 0.58, -0.11, 0.4],
  },
  {
    _id: ObjectId(),
    email: "alan@example.com",
    fullName: "Alan Turing",
    plan: "pro",
    faceEmbedding: [-0.5, 0.2, -0.1, 0.8, 0.3, -0.4, 0.6, -0.2],
  },
]);

const users = db.users.find().toArray();

db.punches.insertMany([
  {
    userId: users[0]._id,
    type: "clock-in",
    timestamp: new Date(),
    // GeoJSON — paste this cell (or right-click "Send to Geo Viewer") to
    // see it rendered on a map.
    location: { type: "Point", coordinates: [-122.0835, 37.4222] },
  },
  {
    userId: users[1]._id,
    type: "clock-in",
    timestamp: new Date(),
    location: { type: "Point", coordinates: [-122.084, 37.4219] },
  },
]);

print("Seeded mongobak_test: " + db.users.countDocuments() + " users, " + db.punches.countDocuments() + " punches");
