export namespace ai {
	
	export class OllamaModel {
	    name: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new OllamaModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.size = source["size"];
	    }
	}

}

export namespace dashboard {

	export class SavedQuery {
	    id: string;
	    name: string;
	    connection: string;
	    database: string;
	    sqlText: string;
	    createdAt: string;

	    static createFrom(source: any = {}) {
	        return new SavedQuery(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.connection = source["connection"];
	        this.database = source["database"];
	        this.sqlText = source["sqlText"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class Widget {
	    id: string;
	    title: string;
	    queryId: string;
	    chartType: string;
	    xColumn: string;
	    yColumns: string[];
	    createdAt: string;

	    static createFrom(source: any = {}) {
	        return new Widget(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.queryId = source["queryId"];
	        this.chartType = source["chartType"];
	        this.xColumn = source["xColumn"];
	        this.yColumns = source["yColumns"];
	        this.createdAt = source["createdAt"];
	    }
	}

}

export namespace depmanager {
	
	export class OllamaStatus {
	    Installed: boolean;
	    Running: boolean;
	
	    static createFrom(source: any = {}) {
	        return new OllamaStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Installed = source["Installed"];
	        this.Running = source["Running"];
	    }
	}

}

export namespace engine {

	export class Caps {
	    sql: boolean;
	    documents: boolean;
	    aggregation: boolean;
	    foreignKeys: boolean;
	    snapshots: boolean;

	    static createFrom(source: any = {}) {
	        return new Caps(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sql = source["sql"];
	        this.documents = source["documents"];
	        this.aggregation = source["aggregation"];
	        this.foreignKeys = source["foreignKeys"];
	        this.snapshots = source["snapshots"];
	    }
	}
	export class Cell {
	    type: string;
	    display: string;
	    raw?: any;
	
	    static createFrom(source: any = {}) {
	        return new Cell(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.display = source["display"];
	        this.raw = source["raw"];
	    }
	}
	export class Column {
	    name: string;
	    dataType: string;
	    nullable: boolean;
	    isPk: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Column(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.dataType = source["dataType"];
	        this.nullable = source["nullable"];
	        this.isPk = source["isPk"];
	    }
	}
	export class ForeignKey {
	    column: string;
	    refTable: string;
	    refColumn: string;
	
	    static createFrom(source: any = {}) {
	        return new ForeignKey(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.column = source["column"];
	        this.refTable = source["refTable"];
	        this.refColumn = source["refColumn"];
	    }
	}
	export class SQLResult {
	    columns: string[];
	    rows: any[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new SQLResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.columns = source["columns"];
	        this.rows = source["rows"];
	        this.total = source["total"];
	    }
	}
	export class TableSchema {
	    name: string;
	    columns: Column[];
	    foreignKeys: ForeignKey[];
	
	    static createFrom(source: any = {}) {
	        return new TableSchema(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.columns = this.convertValues(source["columns"], Column);
	        this.foreignKeys = this.convertValues(source["foreignKeys"], ForeignKey);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class AISettingsInfo {
	    providerId: string;
	    model: string;
	    ollamaHost: string;
	    hasApiKey: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AISettingsInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.providerId = source["providerId"];
	        this.model = source["model"];
	        this.ollamaHost = source["ollamaHost"];
	        this.hasApiKey = source["hasApiKey"];
	    }
	}
	export class CollectionDiffSummary {
	    name: string;
	    addedCount: number;
	    modifiedCount: number;
	    removedCount: number;
	
	    static createFrom(source: any = {}) {
	        return new CollectionDiffSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.addedCount = source["addedCount"];
	        this.modifiedCount = source["modifiedCount"];
	        this.removedCount = source["removedCount"];
	    }
	}
	export class CollectionInfo {
	    name: string;
	    docCount: number;
	    storageSize: number;
	
	    static createFrom(source: any = {}) {
	        return new CollectionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.docCount = source["docCount"];
	        this.storageSize = source["storageSize"];
	    }
	}
	export class ConnectionInfo {
	    name: string;
	    redactedUri: string;
	    engine: string;
	    capabilities: engine.Caps;
	    environment: string;
	    readOnly: boolean;
	    tenantSessionVar: string;
	    tenantValue: string;
	    createdAt: string;

	    static createFrom(source: any = {}) {
	        return new ConnectionInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.redactedUri = source["redactedUri"];
	        this.engine = source["engine"];
	        this.capabilities = new engine.Caps(source["capabilities"]);
	        this.environment = source["environment"];
	        this.readOnly = source["readOnly"];
	        this.tenantSessionVar = source["tenantSessionVar"];
	        this.tenantValue = source["tenantValue"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class ConnectionInput {
	    name: string;
	    uri: string;
	    engine: string;
	    environment: string;
	    readOnly: boolean;
	    sshHost: string;
	    sshUser: string;
	    sshPassword: string;
	    sshPrivateKey: string;
	    tenantSessionVar: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.uri = source["uri"];
	        this.engine = source["engine"];
	        this.environment = source["environment"];
	        this.readOnly = source["readOnly"];
	        this.sshHost = source["sshHost"];
	        this.sshUser = source["sshUser"];
	        this.sshPassword = source["sshPassword"];
	        this.sshPrivateKey = source["sshPrivateKey"];
	        this.tenantSessionVar = source["tenantSessionVar"];
	    }
	}
	export class DependencyStatus {
	    name: string;
	    description: string;
	    installed: boolean;
	    version?: string;
	
	    static createFrom(source: any = {}) {
	        return new DependencyStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.installed = source["installed"];
	        this.version = source["version"];
	    }
	}
	export class DiffChangePage {
	    ids: string[];
	    total: number;
	    offset: number;
	
	    static createFrom(source: any = {}) {
	        return new DiffChangePage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ids = source["ids"];
	        this.total = source["total"];
	        this.offset = source["offset"];
	    }
	}
	export class DiffSummaryResult {
	    fromId: string;
	    toId: string;
	    collections: CollectionDiffSummary[];
	
	    static createFrom(source: any = {}) {
	        return new DiffSummaryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fromId = source["fromId"];
	        this.toId = source["toId"];
	        this.collections = this.convertValues(source["collections"], CollectionDiffSummary);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class IncomingForeignKey {
	    table: string;
	    column: string;
	
	    static createFrom(source: any = {}) {
	        return new IncomingForeignKey(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.table = source["table"];
	        this.column = source["column"];
	    }
	}
	export class IndexInfo {
	    name: string;
	    keysJson: string;
	    unique: boolean;
	
	    static createFrom(source: any = {}) {
	        return new IndexInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.keysJson = source["keysJson"];
	        this.unique = source["unique"];
	    }
	}
	export class QueryResult {
	    documents: string[];
	    total: number;
	    skip: number;
	    limit: number;
	
	    static createFrom(source: any = {}) {
	        return new QueryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.documents = source["documents"];
	        this.total = source["total"];
	        this.skip = source["skip"];
	        this.limit = source["limit"];
	    }
	}
	export class VectorComparison {
	    dimensions: number;
	    cosine: number;
	    euclidean: number;

	    static createFrom(source: any = {}) {
	        return new VectorComparison(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dimensions = source["dimensions"];
	        this.cosine = source["cosine"];
	        this.euclidean = source["euclidean"];
	    }
	}
	export class TableInfo {
	    name: string;
	    rowCount: number;
	    storageSize: number;
	
	    static createFrom(source: any = {}) {
	        return new TableInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.rowCount = source["rowCount"];
	        this.storageSize = source["storageSize"];
	    }
	}

}

export namespace safeguard {
	
	export class Classification {
	    risk: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new Classification(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.risk = source["risk"];
	        this.reason = source["reason"];
	    }
	}

}

export namespace schemadiff {

	export class Column {
	    name: string;
	    dataType: string;
	    nullable: boolean;
	    isPk: boolean;

	    static createFrom(source: any = {}) {
	        return new Column(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.dataType = source["dataType"];
	        this.nullable = source["nullable"];
	        this.isPk = source["isPk"];
	    }
	}
	export class ColumnDiff {
	    name: string;
	    change: string;
	    before?: Column;
	    after?: Column;

	    static createFrom(source: any = {}) {
	        return new ColumnDiff(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.change = source["change"];
	        this.before = source["before"];
	        this.after = source["after"];
	    }
	}
	export class TableDiff {
	    table: string;
	    change: string;
	    columns: ColumnDiff[];

	    static createFrom(source: any = {}) {
	        return new TableDiff(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.table = source["table"];
	        this.change = source["change"];
	        this.columns = source["columns"];
	    }
	}
	export class Migration {
	    sql: string;
	    warnings: string[];

	    static createFrom(source: any = {}) {
	        return new Migration(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sql = source["sql"];
	        this.warnings = source["warnings"];
	    }
	}

}

export namespace migrations {

	export class SaveResult {
	    filePath: string;
	    committed: boolean;
	    gitOutput?: string;

	    static createFrom(source: any = {}) {
	        return new SaveResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filePath = source["filePath"];
	        this.committed = source["committed"];
	        this.gitOutput = source["gitOutput"];
	    }
	}

}

export namespace snapshot {

	export class GCResult {
	    manifestsDeleted: number;
	    objectsDeleted: number;
	
	    static createFrom(source: any = {}) {
	        return new GCResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.manifestsDeleted = source["manifestsDeleted"];
	        this.objectsDeleted = source["objectsDeleted"];
	    }
	}
	export class Summary {
	    id: string;
	    connection: string;
	    database: string;
	    message: string;
	    tags?: string[];
	    createdAt: string;
	    parentId?: string;
	    docCount: number;
	    newObjects: number;
	
	    static createFrom(source: any = {}) {
	        return new Summary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.connection = source["connection"];
	        this.database = source["database"];
	        this.message = source["message"];
	        this.tags = source["tags"];
	        this.createdAt = source["createdAt"];
	        this.parentId = source["parentId"];
	        this.docCount = source["docCount"];
	        this.newObjects = source["newObjects"];
	    }
	}

}

export namespace store {
	
	export class Backup {
	    id: string;
	    connection: string;
	    database: string;
	    fileName: string;
	    sizeBytes: number;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Backup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.connection = source["connection"];
	        this.database = source["database"];
	        this.fileName = source["fileName"];
	        this.sizeBytes = source["sizeBytes"];
	        this.createdAt = source["createdAt"];
	    }
	}

}

