export namespace main {
	
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
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.redactedUri = source["redactedUri"];
	        this.createdAt = source["createdAt"];
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

